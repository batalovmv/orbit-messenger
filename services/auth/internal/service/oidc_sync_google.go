// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"fmt"

	"golang.org/x/oauth2/google"
	admin "google.golang.org/api/admin/directory/v1"
	"google.golang.org/api/option"
)

// googleDirectoryClient lists active Google Workspace users via the
// Admin SDK Directory API with a service account.
type googleDirectoryClient struct {
	svc    *admin.Service
	domain string
}

// NewGoogleDirectoryClient parses the service-account JSON key in saJSON
// and constructs an Admin SDK client authorised for
// admin.directory.user.readonly in the given customer domain.
//
// The service account must have domain-wide delegation enabled and the
// scope https://www.googleapis.com/auth/admin.directory.user.readonly
// granted in the Google Workspace admin console.
func NewGoogleDirectoryClient(ctx context.Context, saJSON string, domain string) (DirectoryClient, error) {
	if saJSON == "" {
		return nil, fmt.Errorf("google directory client: service account JSON is empty")
	}
	if domain == "" {
		return nil, fmt.Errorf("google directory client: domain is empty")
	}

	creds, err := google.CredentialsFromJSON(
		ctx,
		[]byte(saJSON),
		admin.AdminDirectoryUserReadonlyScope,
	)
	if err != nil {
		return nil, fmt.Errorf("google directory client: parse credentials: %w", err)
	}

	svc, err := admin.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("google directory client: create service: %w", err)
	}

	return &googleDirectoryClient{svc: svc, domain: domain}, nil
}

// ListActiveSubjects pages through all non-suspended, non-archived users in
// the domain and returns their Google user IDs (which are the OIDC subjects
// for Google Workspace). A transient error mid-page propagates immediately —
// callers treat any error as "unknown state, deactivate nobody".
func (g *googleDirectoryClient) ListActiveSubjects(ctx context.Context) ([]string, error) {
	var subjects []string
	pageToken := ""

	for {
		call := g.svc.Users.List().
			Domain(g.domain).
			MaxResults(500).
			Fields("nextPageToken,users(id,suspended,archived)").
			Context(ctx)

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("google directory client: list users: %w", err)
		}

		for _, u := range resp.Users {
			if u.Suspended || u.Archived {
				continue
			}
			subjects = append(subjects, u.Id)
		}

		pageToken = resp.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return subjects, nil
}
