package routes

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	domain "github.com/slidebolt/sb-domain"
)

type SecurityGroupsBody struct {
	Groups []domain.SecurityGroupDefinition `json:"groups"`
}

type SecurityGroupsOutput struct{ Body SecurityGroupsBody }

func RegisterAuth(api huma.API) {
	huma.Register(api, huma.Operation{
		Method: "GET", Path: "/auth/security-groups", Summary: "List security groups",
		Description: "Returns the code-owned groups used to classify API resources.", Tags: []string{"authentication"},
	}, func(context.Context, *struct{}) (*SecurityGroupsOutput, error) {
		return &SecurityGroupsOutput{Body: SecurityGroupsBody{Groups: domain.SecurityGroups()}}, nil
	})
}
