package main

import (
	"database/sql"
	"errors"
)

var errProjectNotFound = errors.New("project not found")

type project struct {
	publicID          string
	crowdinProjectID  string
	ciphertext, nonce []byte
	revoked           bool
}

func getProject(db *sql.DB, publicID string) (project, error) {
	var p project
	var revoked int
	err := db.QueryRow(
		`SELECT public_id, crowdin_project_id, ciphertext, nonce, revoked FROM projects WHERE public_id = ?`,
		publicID,
	).Scan(&p.publicID, &p.crowdinProjectID, &p.ciphertext, &p.nonce, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return project{}, errProjectNotFound
	}
	if err != nil {
		return project{}, err
	}
	p.revoked = revoked != 0
	if p.revoked {
		return project{}, errProjectNotFound
	}
	return p, nil
}

func insertProject(db *sql.DB, publicID, crowdinProjectID string, ciphertext, nonce []byte, createdAt int64, revokeTokenHash string) error {
	_, err := db.Exec(
		`INSERT INTO projects (public_id, crowdin_project_id, ciphertext, nonce, key_version, created_at, revoked, revoke_token_hash)
         VALUES (?, ?, ?, ?, 1, ?, 0, ?)`,
		publicID, crowdinProjectID, ciphertext, nonce, createdAt, revokeTokenHash,
	)
	return err
}

// getProjectByRevokeTokenHash looks up a project by its revoke token hash.
// Unlike getProject, it distinguishes "not found" from "found but already
// revoked" so the revoke handler can report which happened.
func getProjectByRevokeTokenHash(db *sql.DB, hash string) (publicID string, revoked bool, err error) {
	var r int
	err = db.QueryRow(
		`SELECT public_id, revoked FROM projects WHERE revoke_token_hash = ?`,
		hash,
	).Scan(&publicID, &r)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, errProjectNotFound
	}
	if err != nil {
		return "", false, err
	}
	return publicID, r != 0, nil
}

func revokeProject(db *sql.DB, publicID string) error {
	_, err := db.Exec(`UPDATE projects SET revoked = 1 WHERE public_id = ?`, publicID)
	return err
}
