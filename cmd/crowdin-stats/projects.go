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

func insertProject(db *sql.DB, publicID, crowdinProjectID string, ciphertext, nonce []byte, createdAt int64) error {
	_, err := db.Exec(
		`INSERT INTO projects (public_id, crowdin_project_id, ciphertext, nonce, key_version, created_at, revoked)
         VALUES (?, ?, ?, ?, 1, ?, 0)`,
		publicID, crowdinProjectID, ciphertext, nonce, createdAt,
	)
	return err
}
