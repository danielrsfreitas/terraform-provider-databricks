# NEXT CHANGELOG

## Release v1.142.0

### Important Changes

### Breaking Changes

### New Features and Improvements
* `databricks_connection` now supports referencing Unity Catalog secrets through the `secrets` argument ([#6126](https://github.com/databricks/terraform-provider-databricks/pull/6126)).
* Add resource and data sources for `databricks_lakewatch_datasource`.
* Add resource and data source for `databricks_lakewatch_datasource_config`.
* Add resource and data source for `databricks_lakewatch_rule_config`.
* Add resource and data source for `databricks_lakewatch_alert_config`.
* Add resource and data source for `databricks_lakewatch_common_config`.
* Add resource and data sources for `databricks_lakewatch_rule`.

* Add `databricks_app_slack` and `databricks_app_teams` configuration blocks to `databricks_notification_destination`, and accept `DATABRICKS_APP_SLACK` and `DATABRICKS_APP_TEAMS` as `type` in the `databricks_notification_destinations` data source ([#6131](https://github.com/databricks/terraform-provider-databricks/pull/6131)).

### Bug Fixes

* Fix data races when concurrent resource operations access the unified provider's client caches ([#6120](https://github.com/databricks/terraform-provider-databricks/pull/6120)).

* Preserve `databricks_pipeline.expected_last_modified` during refresh to avoid post-apply drift ([#6133](https://github.com/databricks/terraform-provider-databricks/pull/6133)).

* Retry `databricks_access_control_rule_set` create, update and delete with backoff on `RESOURCE_CONFLICT`. Previously create did not retry, and update and delete retried only once ([#6161](https://github.com/databricks/terraform-provider-databricks/issues/6161)).

### Documentation

### Exporter

### Internal Changes
