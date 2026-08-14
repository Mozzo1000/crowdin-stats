package main

import "errors"

var errRateLimited = errors.New("rate limited")

var errProjectNotFound = errors.New("project not found")

// Sentinel classifications for Crowdin API failures, so callers several
// layers up (e.g. handleEmbedError) can distinguish "your token is dead"
// from "Crowdin had a blip" via errors.Is without string-matching messages.
var (
	errCrowdinAuthInvalid     = errors.New("crowdin token invalid or lacks access to this project")
	errCrowdinProjectNotFound = errors.New("crowdin project not found")
)
