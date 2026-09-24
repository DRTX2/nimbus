@e2e @regression
Feature: Reliable workflow execution
  Operators automate responses as versioned DAG workflows. Nimbus must accept
  each event once per idempotency key, run independent branches in parallel,
  and report every step outcome.

  Background:
    Given a fresh tenant with an owner

  Scenario: Independent branches run after their shared dependency
    Given I create a workflow "Order operations" triggered by "order.created" with steps:
      | id       | type | dependsOn |
      | validate | noop |           |
      | notify   | noop | validate  |
      | audit    | noop | validate  |
    When I ingest an "order.created" event with idempotency key "order-42"
    Then the ingestion should be accepted as new
    And the execution should succeed within 15 seconds
    And step "validate" should be "succeeded" after 1 attempt
    And steps "notify" and "audit" should both start after "validate" finished

  Scenario: Repeating an event with the same idempotency key is harmless
    Given I create a workflow "Order operations" triggered by "order.created" with steps:
      | id       | type |
      | validate | noop |
    When I ingest an "order.created" event with idempotency key "order-7"
    And I ingest an "order.created" event with idempotency key "order-7"
    Then the ingestion should be accepted as a duplicate
    And it should reference the same execution as before

  Scenario: Events must match the workflow trigger
    Given I create a workflow "Order operations" triggered by "order.created" with steps:
      | id       | type |
      | validate | noop |
    When I ingest an "invoice.paid" event with idempotency key "wrong-type"
    Then the response status should be 400

  Scenario Outline: Invalid workflow contracts are rejected before they are stored (<case>)
    When I try to create a workflow "Broken" triggered by "order.created" with steps:
      | id  | type   | dependsOn   | url   |
      | one | <type> | <dependsOn> | <url> |
      | two | noop   | one         |       |
    Then the response status should be <status>

    Examples:
      | case               | type | dependsOn | url                       | status |
      | dependency cycle   | noop | two       |                           | 400    |
      | missing dependency | noop | ghost     |                           | 400    |
      | self dependency    | noop | one       |                           | 400    |
      | non-http scheme    | http |           | ftp://example.com/hook    | 400    |
      | embedded password  | http |           | https://u:p@example.com/x | 400    |
      | unknown step type  | cron |           |                           | 422    |

  Scenario: New versions apply only after explicit activation
    Given I create a workflow "Versioned" triggered by "order.created" with steps:
      | id       | type |
      | validate | noop |
    When I send a POST request to "/v1/workflows/{workflowId}/versions" with:
      """
      {"schemaVersion": 1, "eventType": "order.updated", "steps": [{"id": "sync", "type": "noop"}]}
      """
    Then the response status should be 201
    And the response field "version" should be "2"
    When I ingest an "order.updated" event with idempotency key "before-activation"
    Then the response status should be 400
    When I send a POST request to "/v1/workflows/{workflowId}/versions/2/activations"
    Then the response status should be 200
    When I ingest an "order.updated" event with idempotency key "after-activation"
    Then the ingestion should be accepted as new
    And the execution should succeed within 15 seconds

  Scenario: Executions can be browsed page by page
    Given I create a workflow "Paged" triggered by "order.created" with steps:
      | id       | type |
      | validate | noop |
    And I ingest an "order.created" event with idempotency key "page-1"
    And I ingest an "order.created" event with idempotency key "page-2"
    And I ingest an "order.created" event with idempotency key "page-3"
    When I send a GET request to "/v1/executions?limit=2"
    Then the response status should be 200
    And the response should list 2 items
    And I remember the response field "nextCursor" as "cursor"
    When I send a GET request to "/v1/executions?limit=2&cursor={cursor}"
    Then the response should list 1 item
