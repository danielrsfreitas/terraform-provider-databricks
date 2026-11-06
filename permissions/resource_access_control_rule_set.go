package permissions

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/databricks/databricks-sdk-go/apierr"
	"github.com/databricks/databricks-sdk-go/service/iam"
	"github.com/databricks/terraform-provider-databricks/common"
	"github.com/databricks/terraform-provider-databricks/internal/retrier"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// ruleSetConflictMaxAttempts bounds how many times a rule set write is attempted
// when it keeps failing with RESOURCE_CONFLICT.
const ruleSetConflictMaxAttempts = 5

// ruleSetConflictBackoff spaces out those attempts. It is a variable so tests
// can make the waits negligible.
var ruleSetConflictBackoff = retrier.BackoffPolicy{Initial: time.Second, Maximum: 8 * time.Second}

func isRuleSetConflict(err error) bool {
	var aerr *apierr.APIError
	return errors.As(err, &aerr) && aerr.StatusCode == http.StatusConflict && aerr.ErrorCode == "RESOURCE_CONFLICT"
}

func ResourceAccessControlRuleSet() common.Resource {
	s := common.StructToSchema(
		iam.RuleSetUpdateRequest{},
		func(m map[string]*schema.Schema) map[string]*schema.Schema {
			m["etag"].Required = false
			m["etag"].Computed = true
			m["grant_rules"].Type = schema.TypeSet
			common.MustSchemaPath(m, "grant_rules", "principals").Type = schema.TypeSet
			m["name"].ForceNew = true
			common.AddApiField(m)

			return m
		})
	common.AddNamespaceInSchema(s)
	common.NamespaceCustomizeSchemaMap(s)
	readFromWsOrAcc := func(ctx context.Context, d *schema.ResourceData, c *common.DatabricksClient, getRuleSetReq iam.GetRuleSetRequest) (*iam.RuleSetResponse, error) {
		if common.IsAccountLevel(d, c) {
			accountClient, err := c.AccountClient()
			if err != nil {
				return nil, err
			}
			return accountClient.AccessControl.GetRuleSet(ctx, getRuleSetReq)
		}
		workspaceClient, err := c.WorkspaceClient()
		if err != nil {
			return nil, err
		}
		return workspaceClient.AccountAccessControlProxy.GetRuleSet(ctx, getRuleSetReq)
	}
	updateThroughWsOrAcc := func(ctx context.Context, d *schema.ResourceData, c *common.DatabricksClient, updateRuleSetReq iam.UpdateRuleSetRequest) (*iam.RuleSetResponse, error) {
		if common.IsAccountLevel(d, c) {
			accountClient, err := c.AccountClient()
			if err != nil {
				return nil, err
			}
			return accountClient.AccessControl.UpdateRuleSet(ctx, updateRuleSetReq)
		}
		workspaceClient, err := c.WorkspaceClient()
		if err != nil {
			return nil, err
		}
		return workspaceClient.AccountAccessControlProxy.UpdateRuleSet(ctx, updateRuleSetReq)
	}
	fetchLatestEtag := func(ctx context.Context, d *schema.ResourceData, c *common.DatabricksClient, name string) (string, error) {
		ruleSetGetRes, err := readFromWsOrAcc(ctx, d, c, iam.GetRuleSetRequest{
			Name: name,
			Etag: "",
		})
		if err != nil {
			return "", err
		}
		return ruleSetGetRes.Etag, nil
	}
	// updateWithLatestEtag writes the rule set using its latest etag. Right after
	// another write to the same rule set (e.g. the Delete half of a replacement),
	// the etag read can still be stale and the write fails with RESOURCE_CONFLICT,
	// so it re-reads the etag and retries with backoff.
	updateWithLatestEtag := func(ctx context.Context, d *schema.ResourceData, c *common.DatabricksClient,
		ruleSetUpdateReq iam.UpdateRuleSetRequest) (*iam.RuleSetResponse, error) {
		attempts := 0
		retryOnConflict := retrier.RetryIf(ruleSetConflictBackoff, func(_ *iam.RuleSetResponse, err error) bool {
			attempts++
			return attempts < ruleSetConflictMaxAttempts && isRuleSetConflict(err)
		})
		return retrier.Run(ctx, retryOnConflict, func(ctx context.Context) (*iam.RuleSetResponse, error) {
			latestEtag, err := fetchLatestEtag(ctx, d, c, ruleSetUpdateReq.Name)
			if err != nil {
				return nil, err
			}
			ruleSetUpdateReq.RuleSet.Etag = latestEtag
			return updateThroughWsOrAcc(ctx, d, c, ruleSetUpdateReq)
		})
	}
	return common.Resource{
		IsDual: true,
		Schema: s,
		CustomizeDiff: func(ctx context.Context, d *schema.ResourceDiff, c *common.DatabricksClient) error {
			return common.CustomizeDiffDualResources(ctx, d, c)
		},
		CanSkipReadAfterCreateAndUpdate: func(_ *schema.ResourceData) bool {
			return true
		},
		Create: func(ctx context.Context, d *schema.ResourceData, c *common.DatabricksClient) error {
			c, err := c.DatabricksClientForDualResource(ctx, d)
			if err != nil {
				return err
			}
			var ruleSetUpdateReq iam.UpdateRuleSetRequest
			common.DataToStructPointer(d, s, &ruleSetUpdateReq.RuleSet)
			ruleSetUpdateReq.Name = ruleSetUpdateReq.RuleSet.Name
			response, err := updateWithLatestEtag(ctx, d, c, ruleSetUpdateReq)
			if err != nil {
				return err
			}
			// Store empty etag in state as we will always use the latest etag
			response.Etag = ""
			err = common.StructToData(response, s, d)
			if err != nil {
				return err
			}
			d.SetId(ruleSetUpdateReq.Name)
			return nil
		},
		Read: func(ctx context.Context, d *schema.ResourceData, c *common.DatabricksClient) error {
			c, err := c.DatabricksClientForDualResource(ctx, d)
			if err != nil {
				return err
			}
			data, err := readFromWsOrAcc(ctx, d, c, iam.GetRuleSetRequest{
				Name: d.Id(),
				Etag: "",
			})
			if err != nil {
				return err
			}
			// Store empty etag in state as we will always use the latest etag
			data.Etag = ""
			if err := common.StructToData(data, s, d); err != nil {
				return err
			}

			// StructToData intentionally skips optional empty collections. For this
			// authoritative resource, an empty grant_rules response means remote drift
			// removed all rules, so clear stale Terraform state explicitly.
			if len(data.GrantRules) == 0 {
				return d.Set("grant_rules", []interface{}{})
			}
			return nil
		},
		Update: func(ctx context.Context, d *schema.ResourceData, c *common.DatabricksClient) error {
			c, err := c.DatabricksClientForDualResource(ctx, d)
			if err != nil {
				return err
			}
			var ruleSetUpdateReq iam.UpdateRuleSetRequest
			common.DataToStructPointer(d, s, &ruleSetUpdateReq.RuleSet)
			ruleSetUpdateReq.Name = ruleSetUpdateReq.RuleSet.Name
			response, err := updateWithLatestEtag(ctx, d, c, ruleSetUpdateReq)
			if err != nil {
				return err
			}
			// Storing empty etag in state as we will always use the latest etag
			response.Etag = ""
			err = common.StructToData(response, s, d)
			if err != nil {
				return err
			}
			d.SetId(ruleSetUpdateReq.Name)
			return nil
		},
		Delete: func(ctx context.Context, d *schema.ResourceData, c *common.DatabricksClient) error {
			c, err := c.DatabricksClientForDualResource(ctx, d)
			if err != nil {
				return err
			}
			// we remove all grant rules. Account admins will still be able to update rule set
			_, err = updateWithLatestEtag(ctx, d, c, iam.UpdateRuleSetRequest{
				Name: d.Id(),
				RuleSet: iam.RuleSetUpdateRequest{
					Name: d.Id(),
				},
			})
			if err != nil {
				return err
			}
			return nil
		},
	}
}
