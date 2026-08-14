package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/crowdin/crowdin-api-client-go/crowdin"
	"github.com/crowdin/crowdin-api-client-go/crowdin/model"
	"golang.org/x/time/rate"
)

// outboundLimiter bounds aggregate outbound calls to Crowdin's API across all
// projects, independent of the per-project rate limits enforced on embed
// routes (see ratelimit.go). This protects against many projects' caches
// expiring around the same time and hammering Crowdin at once.
var outboundLimiter = rate.NewLimiter(rate.Limit(5), 10)

// LanguageProgress is the render-ready shape consumed by renderTableSVG and,
// aggregated across all languages, by the overall.svg renderers. It carries
// both translation and approval figures so callers can pick which one to
// render (see ProgressType) without a second Crowdin API call.
type LanguageProgress struct {
	LanguageName      string
	LanguageID        string // Crowdin/ISO language code, e.g. "fr" — used to match the `languages` pin list
	Percent           int    // translation progress %
	ApprovalPercent   int
	WordsTotal        int
	WordsTranslated   int
	WordsApproved     int
	PhrasesTotal      int // "phrases" is Crowdin's internal name for strings
	PhrasesTranslated int
	PhrasesApproved   int
}

// Contributor is the render-ready shape consumed by renderContributorsSVG.
type Contributor struct {
	Username  string
	FullName  string
	AvatarURL string
	Amount    int64
}

// crowdinRequestSetup parses projectID, builds an official Crowdin API
// client for a single request, and blocks until outboundLimiter admits the
// call — the boilerplate every handler needs before it can talk to Crowdin.
func crowdinRequestSetup(ctx context.Context, token, projectID string) (*crowdin.Client, int, error) {
	id, err := strconv.Atoi(projectID)
	if err != nil {
		return nil, 0, errors.New("project ID must be numeric")
	}

	client, err := crowdin.NewClient(token)
	if err != nil {
		return nil, 0, err
	}

	if err := outboundLimiter.Wait(ctx); err != nil {
		return nil, 0, err
	}

	return client, id, nil
}

// friendlyAPIError turns a Crowdin client error into a short, specific
// error safe to show to end users during onboarding. Known failure modes map
// to the errCrowdin* sentinels so callers can branch on them with errors.Is;
// anything unrecognized gets a generic message instead of the raw Go/HTTP
// error text, which can leak internal detail (URLs, headers) to a
// non-technical user. The raw error is still logged here for debugging.
func friendlyAPIError(err error) error {
	var errResp *model.ErrorResponse
	if errors.As(err, &errResp) {
		switch errResp.Response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return errCrowdinAuthInvalid
		case http.StatusNotFound:
			return errCrowdinProjectNotFound
		case http.StatusTooManyRequests:
			return fmt.Errorf("%w: Crowdin rate limit hit, try again shortly", errRateLimited)
		}
	}
	var valErr *model.ValidationErrorResponse
	if errors.As(err, &valErr) {
		return fmt.Errorf("invalid request: %s", valErr.Error())
	}
	slog.Warn("unrecognized crowdin api error", "error", err)
	return errors.New("could not reach Crowdin, please try again")
}

// ValidateProject confirms a token is valid and scoped to the given project.
func ValidateProject(ctx context.Context, token, projectID string) error {
	client, id, err := crowdinRequestSetup(ctx, token, projectID)
	if err != nil {
		return err
	}

	_, _, err = client.Projects.Get(ctx, id)
	if err != nil {
		return friendlyAPIError(err)
	}
	return nil
}

// ProjectSummary is the minimal project shape shown in the onboarding
// project picker once a token has been entered.
type ProjectSummary struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ListProjects returns the projects a token has access to, for the
// onboarding project picker. Granular-access PATs only ever see the
// project(s) they were scoped to, so this list is naturally already
// filtered to what the user is allowed to pick from.
func ListProjects(ctx context.Context, token string) ([]ProjectSummary, error) {
	client, err := crowdin.NewClient(token)
	if err != nil {
		return nil, err
	}

	if err := outboundLimiter.Wait(ctx); err != nil {
		return nil, err
	}
	projects, _, err := client.Projects.List(ctx, &model.ProjectsListOptions{
		ListOptions: model.ListOptions{Limit: 500},
	})
	if err != nil {
		return nil, friendlyAPIError(err)
	}

	out := make([]ProjectSummary, 0, len(projects))
	for _, p := range projects {
		out = append(out, ProjectSummary{ID: p.ID, Name: p.Name})
	}
	return out, nil
}

// fetchProjectOwnerID returns the Crowdin user ID of the project's owner,
// used to filter them out of the contributors grid when hideOwner is set —
// the owner shows up in the top-members report like any other translator,
// but isn't a "contributor" in the sense the embed is meant to celebrate.
func fetchProjectOwnerID(ctx context.Context, token, projectID string) (int64, error) {
	client, id, err := crowdinRequestSetup(ctx, token, projectID)
	if err != nil {
		return 0, err
	}

	project, _, err := client.Projects.Get(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("fetch project owner: %w", friendlyAPIError(err))
	}
	return int64(project.UserID), nil
}

// FetchLanguageProgress retrieves per-language translation progress for a project.
func FetchLanguageProgress(ctx context.Context, token, projectID string) ([]LanguageProgress, error) {
	client, id, err := crowdinRequestSetup(ctx, token, projectID)
	if err != nil {
		return nil, err
	}

	progress, _, err := client.TranslationStatus.GetProjectProgress(ctx, id, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch translation progress: %w", friendlyAPIError(err))
	}

	out := make([]LanguageProgress, 0, len(progress))
	for _, p := range progress {
		name, langID := "", ""
		if p.Language != nil {
			name = p.Language.Name
			langID = p.Language.ID
		} else if p.LanguageID != nil {
			name = *p.LanguageID
			langID = *p.LanguageID
		}
		out = append(out, LanguageProgress{
			LanguageName:      name,
			LanguageID:        langID,
			Percent:           p.TranslationProgress,
			ApprovalPercent:   p.ApprovalProgress,
			WordsTotal:        p.Words["total"],
			WordsTranslated:   p.Words["translated"],
			WordsApproved:     p.Words["approved"],
			PhrasesTotal:      p.Phrases["total"],
			PhrasesTranslated: p.Phrases["translated"],
			PhrasesApproved:   p.Phrases["approved"],
		})
	}
	return out, nil
}

// flexibleInt decodes a JSON field that Crowdin's report export emits as a
// quoted string (observed on user.id, translated, and approved) rather than
// a bare number — a plain int or json.Number field hard-fails on that quoted
// form, which was silently breaking every contributors.svg render.
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
		ID        flexibleInt `json:"id"`
		Username  string      `json:"username"`
		FullName  string      `json:"fullName"`
		AvatarURL string      `json:"avatarUrl"`
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
// When hideOwner is set, the project owner is excluded from the result.
// avatarLimit bounds how many avatars are fetched and embedded (see
// embedAvatarsAsDataURIs); pass the same limit the caller will render with,
// so a small `limit` embed doesn't pay to download avatars it will discard.
func FetchTopMembers(ctx context.Context, token, projectID string, unit ReportUnit, hideOwner bool, avatarLimit int) ([]Contributor, error) {
	// Kicked off in parallel with report generation/polling below, so
	// hideOwner doesn't add its own round trip to the embed's latency.
	var ownerID int64
	var ownerErr error
	var ownerDone chan struct{}
	if hideOwner {
		ownerDone = make(chan struct{})
		go func() {
			defer close(ownerDone)
			ownerID, ownerErr = fetchProjectOwnerID(ctx, token, projectID)
		}()
	}

	client, id, err := crowdinRequestSetup(ctx, token, projectID)
	if err != nil {
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
		return nil, fmt.Errorf("generate top-members report: %w", friendlyAPIError(err))
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
			return nil, fmt.Errorf("check report status: %w", friendlyAPIError(err))
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
		return nil, fmt.Errorf("get report download link: %w", friendlyAPIError(err))
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

	if hideOwner {
		<-ownerDone
		if ownerErr != nil {
			return nil, ownerErr
		}
	}

	out := make([]Contributor, 0, len(report.Data))
	for _, row := range report.Data {
		if row.User.Username == "REMOVED_USER" {
			continue
		}
		if hideOwner && int64(row.User.ID) == ownerID {
			continue
		}
		out = append(out, Contributor{
			Username:  row.User.Username,
			FullName:  row.User.FullName,
			AvatarURL: row.User.AvatarURL,
			Amount:    int64(row.Translated) + int64(row.Approved),
		})
	}
	return embedAvatarsAsDataURIs(ctx, out, avatarLimit), nil
}
