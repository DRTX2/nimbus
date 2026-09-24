@smoke
Feature: Deployment smoke check
  After every deployment an operator needs fast evidence that Nimbus is
  reachable, authenticates callers, documents its contract, and can run a
  workflow end to end.

  Background:
    Given a fresh tenant with an owner

  Scenario: The service reports healthy probes
    When I send an unauthenticated GET request to "/health/live"
    Then the response status should be 200
    When I send an unauthenticated GET request to "/health/ready"
    Then the response status should be 200

  Scenario: Protected endpoints reject anonymous callers
    When I send an unauthenticated GET request to "/v1/workflows"
    Then the response status should be 401

  Scenario: The API contract is published
    When I send an unauthenticated GET request to "/openapi.json"
    Then the response status should be 200
    And the response field "info.title" should contain "Nimbus"

  Scenario: The owner can see who they are
    When I send a GET request to "/v1/me"
    Then the response status should be 200
    And the response field "role" should be "owner"

  Scenario: A single-step workflow runs to completion
    When I create a workflow "Smoke check" triggered by "smoke.ping" with steps:
      | id    | type |
      | check | noop |
    And I ingest a "smoke.ping" event with idempotency key "smoke-1"
    Then the ingestion should be accepted as new
    And the execution should succeed within 15 seconds
