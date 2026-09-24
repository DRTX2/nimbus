@regression
Feature: Tenant ingestion quota
  A noisy tenant must not starve others. Ingestion beyond the per-minute quota
  is rejected with 429 so clients back off.

  Scenario: Ingestion above the tenant quota is throttled
    Given a fresh tenant with an owner
    And I create a workflow "Burst" triggered by "burst.event" with steps:
      | id   | type |
      | work | noop |
    When I ingest 31 distinct "burst.event" events
    Then 30 events should be accepted and the rest rejected with status 429
