package db

import (
	"context"
	"time"
)

// AuthenticateCandidate updates a candidate's authKey + lastActivityAt
// (login/logout). Mirrors CommonRepo.AuthenticateUser for the candidate
// credentials column set added alongside RPC auth.
func (ar ApprenticeRepo) AuthenticateCandidate(ctx context.Context, c *Candidate, authKey string) (bool, error) {
	c.AuthKey = authKey
	now := time.Now()
	c.LastActivityAt = &now
	return ar.UpdateCandidate(ctx, c, WithColumns(Columns.Candidate.AuthKey, Columns.Candidate.LastActivityAt))
}

func (ar ApprenticeRepo) UpdateCandidateActivity(ctx context.Context, c *Candidate) (bool, error) {
	now := time.Now()
	c.LastActivityAt = &now
	return ar.UpdateCandidate(ctx, c, WithColumns(Columns.Candidate.LastActivityAt))
}

func (ar ApprenticeRepo) EnabledCandidateByAuthKey(ctx context.Context, authKey string) (*Candidate, error) {
	if authKey == "" {
		return nil, nil
	}
	s := StatusEnabled
	return ar.OneCandidate(ctx, &CandidateSearch{AuthKey: &authKey, StatusID: &s})
}

func (ar ApprenticeRepo) EnabledCandidateByLogin(ctx context.Context, login string) (*Candidate, error) {
	s := StatusEnabled
	return ar.OneCandidate(ctx, &CandidateSearch{Login: &login, StatusID: &s})
}

func (ar ApprenticeRepo) UpdateCandidatePassword(ctx context.Context, c *Candidate) (bool, error) {
	return ar.UpdateCandidate(ctx, c, WithColumns(Columns.Candidate.Password, Columns.Candidate.AuthKey))
}
