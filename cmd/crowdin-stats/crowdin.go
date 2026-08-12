package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/crowdin/crowdin-api-client-go/crowdin"
	"github.com/crowdin/crowdin-api-client-go/crowdin/model"
	"golang.org/x/time/rate"
)

// outboundLimiter bounds aggregate outbound calls to Crowdin's API across all
// projects, independent of the per-project rate limits enforced on badge
// routes (see ratelimit.go). This protects against many projects' caches
// expiring around the same time and hammering Crowdin at once.
var outboundLimiter = rate.NewLimiter(rate.Limit(5), 10)

// LanguageProgress is the render-ready shape consumed by renderTableSVG.
type LanguageProgress struct {
	LanguageName string
	Percent      int
}

// Contributor is the render-ready shape consumed by renderContributorsSVG.
type Contributor struct {
	Username  string
	FullName  string
	AvatarURL string
	Amount    int64
}

// crowdinClientFor builds an official Crowdin API client for a single request.
func crowdinClientFor(token string) (*crowdin.Client, error) {
	return crowdin.NewClient(token)
}

// friendlyAPIError turns a Crowdin client error into a short, specific
// message safe to show to end users during onboarding.
func friendlyAPIError(err error) string {
	var errResp *model.ErrorResponse
	if errors.As(err, &errResp) {
		switch errResp.Response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "token invalid or lacks access to this project"
		case http.StatusNotFound:
			return "project not found — check the project ID"
		case http.StatusTooManyRequests:
			return "Crowdin rate limit hit, try again shortly"
		}
	}
	var valErr *model.ValidationErrorResponse
	if errors.As(err, &valErr) {
		return "invalid request: " + valErr.Error()
	}
	return "could not reach Crowdin: " + err.Error()
}

// ValidateProject confirms a token is valid and scoped to the given project.
func ValidateProject(ctx context.Context, token, projectID string) error {
	id, err := strconv.Atoi(projectID)
	if err != nil {
		return errors.New("project ID must be numeric")
	}

	client, err := crowdinClientFor(token)
	if err != nil {
		return err
	}

	if err := outboundLimiter.Wait(ctx); err != nil {
		return err
	}
	_, _, err = client.Projects.Get(ctx, id)
	if err != nil {
		return errors.New(friendlyAPIError(err))
	}
	return nil
}

// FetchLanguageProgress retrieves per-language translation progress for a project.
func FetchLanguageProgress(ctx context.Context, token, projectID string) ([]LanguageProgress, error) {
	id, err := strconv.Atoi(projectID)
	if err != nil {
		return nil, errors.New("project ID must be numeric")
	}

	client, err := crowdinClientFor(token)
	if err != nil {
		return nil, err
	}

	if err := outboundLimiter.Wait(ctx); err != nil {
		return nil, err
	}
	progress, _, err := client.TranslationStatus.GetProjectProgress(ctx, id, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch translation progress: %s", friendlyAPIError(err))
	}

	out := make([]LanguageProgress, 0, len(progress))
	for _, p := range progress {
		name := ""
		if p.Language != nil {
			name = p.Language.Name
		} else if p.LanguageID != nil {
			name = *p.LanguageID
		}
		out = append(out, LanguageProgress{
			LanguageName: name,
			Percent:      p.TranslationProgress,
		})
	}
	return out, nil
}

// flexibleInt decodes a JSON field that Crowdin's report export sometimes
// emits as a quoted string and sometimes as a bare number (observed across
// report formats/versions) — encoding/json's json.Number only accepts the
// bare-number form and hard-fails on a quoted one, which was silently
// breaking every contributors.svg render.
type flexibleInt int64

func (f *flexibleInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("flexibleInt: %w", err)
	}
	*f = flexibleInt(n)
	return nil
}

// topMembersReportRow mirrors the shape of the JSON exported by the
// "top-members" report. This shape is not modeled by the official Crowdin Go
// client (Download only returns a signed URL to the raw file), so it's
// re-derived here from Crowdin's documented report output.
type topMembersReportRow struct {
	User struct {
		ID        int    `json:"id"`
		Username  string `json:"username"`
		FullName  string `json:"fullName"`
		AvatarURL string `json:"avatarUrl"`
	} `json:"user"`
	Translated flexibleInt `json:"translated"`
	Approved   flexibleInt `json:"approved"`
}

type topMembersReport struct {
	Data []topMembersReportRow `json:"data"`
}

// ReportUnit is the contribution unit the caller wants members ranked by.
type ReportUnit string

const (
	UnitWords      ReportUnit = "words"
	UnitStrings    ReportUnit = "strings"
	UnitCharacters ReportUnit = "characters"
)

func (u ReportUnit) toCrowdinUnit() model.ReportUnit {
	switch u {
	case UnitStrings:
		return model.ReportUnitStrings
	case UnitCharacters:
		return model.ReportUnitChars
	default:
		return model.ReportUnitWords
	}
}

// FetchTopMembers runs the async generate -> poll -> download flow for the
// "top-members" report and returns contributors ranked by the given unit.
func FetchTopMembers(ctx context.Context, token, projectID string, unit ReportUnit) ([]Contributor, error) {
	id, err := strconv.Atoi(projectID)
	if err != nil {
		return nil, errors.New("project ID must be numeric")
	}

	client, err := crowdinClientFor(token)
	if err != nil {
		return nil, err
	}

	if err := outboundLimiter.Wait(ctx); err != nil {
		return nil, err
	}
	status, _, err := client.Reports.Generate(ctx, id, &model.ReportGenerateRequest{
		Name: model.ReportTopMembers,
		Schema: &model.TopMembersSchema{
			Unit:   unit.toCrowdinUnit(),
			Format: model.ReportFormatJSON,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("generate top-members report: %s", friendlyAPIError(err))
	}

	reportID := status.Identifier
	deadline := time.Now().Add(60 * time.Second)
	for status.Status != "finished" {
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for Crowdin report generation")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}

		if err := outboundLimiter.Wait(ctx); err != nil {
			return nil, err
		}
		status, _, err = client.Reports.CheckStatus(ctx, id, reportID)
		if err != nil {
			return nil, fmt.Errorf("check report status: %s", friendlyAPIError(err))
		}
		if status.Status == "failed" {
			return nil, errors.New("Crowdin report generation failed")
		}
	}

	if err := outboundLimiter.Wait(ctx); err != nil {
		return nil, err
	}
	link, _, err := client.Reports.Download(ctx, id, reportID)
	if err != nil {
		return nil, fmt.Errorf("get report download link: %s", friendlyAPIError(err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download report: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download report: unexpected status %d", resp.StatusCode)
	}

	var report topMembersReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, fmt.Errorf("parse report: %w", err)
	}

	out := make([]Contributor, 0, len(report.Data))
	for _, row := range report.Data {
		if row.User.Username == "REMOVED_USER" {
			continue
		}
		out = append(out, Contributor{
			Username:  row.User.Username,
			FullName:  row.User.FullName,
			AvatarURL: row.User.AvatarURL,
			Amount:    int64(row.Translated) + int64(row.Approved),
		})
	}
	return out, nil
}
