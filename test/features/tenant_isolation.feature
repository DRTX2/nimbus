@security @regression
Feature: Tenant isolation
  Tenants share one database. Row-level security bound to the authenticated
  credential must make every other tenant's data invisible.

  Scenario: A tenant cannot read another tenant's workflow or execution
    Given a fresh tenant with an owner
    And I create a workflow "Private" triggered by "secret.event" with steps:
      | id   | type |
      | work | noop |
    And I ingest a "secret.event" event with idempotency key "private-1"
    And I remember the response field "executionId" as "foreignExecution"
    And a fresh tenant with an owner
    When I send a GET request to "/v1/executions/{foreignExecution}"
    Then the response status should be 404
    When I send a GET request to "/v1/workflows/{workflowId}"
    Then the response status should be 404
    When I send a GET request to "/v1/workflows"
    Then the response should list 0 items

  Scenario: A tenant cannot ingest events into another tenant's workflow
    Given a fresh tenant with an owner
    And I create a workflow "Private" triggered by "secret.event" with steps:
      | id   | type |
      | work | noop |
    And a fresh tenant with an owner
    When I ingest a "secret.event" event with idempotency key "cross-tenant"
    Then the response status should be 404
