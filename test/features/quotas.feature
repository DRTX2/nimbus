@regression
Feature: Tenant quotas
  A noisy tenant must not starve others or run up cloud costs. Work beyond a
  tenant quota is rejected with 429 so clients back off.

  Scenario: Ingestion above the tenant quota is throttled
    Given a fresh tenant with an owner
    And I create a workflow "Burst" triggered by "burst.event" with steps:
      | id   | type |
      | work | noop |
    When I ingest 31 distinct "burst.event" events
    Then 30 events should be accepted and the rest rejected with status 429

  Scenario: Concurrent ingestion cannot overshoot the tenant quota
    Given a fresh tenant with an owner
    And I create a workflow "Burst" triggered by "burst.event" with steps:
      | id   | type |
      | work | noop |
    When I ingest 40 distinct "burst.event" events concurrently
    Then exactly 30 events should be accepted and the rest rejected with status 429

  Scenario: CloudWatch checks above the tenant budget are throttled
    Given a fresh tenant with an owner
    And an AWS pipeline "billing"
    When I check the pipeline against CloudWatch 4 times
    Then 3 checks should be attempted and the rest rejected with status 429
    And the audit log should record "pipeline.check"
