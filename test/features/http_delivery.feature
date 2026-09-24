@integration @regression
Feature: Governed HTTP delivery
  HTTP steps deliver events to customer endpoints. Deliveries must be signed,
  carry a stable idempotency key, retry only transient failures, and stop
  dependent work after a permanent failure.

  Background:
    Given a fresh tenant with an owner

  Scenario: A delivery is signed and carries the event payload
    Given the webhook "orders" responds with statuses "204"
    And I create a workflow "Notify" triggered by "order.created" with steps:
      | id     | type | webhook |
      | notify | http | orders  |
    When I ingest an "order.created" event with idempotency key "signed-1"
    Then the execution should succeed within 15 seconds
    And the webhook "orders" should have received 1 delivery
    And every delivery to "orders" should be signed and carry the idempotency key of step "notify"
    And the delivery to "orders" should contain the event payload

  Scenario: Transient failures are retried with the same idempotency key
    Given the webhook "flaky" responds with statuses "503,429,204"
    And I create a workflow "Retrying" triggered by "order.created" with steps:
      | id     | type | webhook | maxAttempts |
      | notify | http | flaky   | 5           |
    When I ingest an "order.created" event with idempotency key "retry-1"
    Then the execution should succeed within 20 seconds
    And step "notify" should be "succeeded" after 3 attempts
    And every delivery to "flaky" should be signed and carry the idempotency key of step "notify"

  Scenario: A permanent failure stops the execution without retrying
    Given the webhook "rejecting" responds with statuses "400"
    And I create a workflow "Failing" triggered by "order.created" with steps:
      | id      | type | webhook   | dependsOn | maxAttempts |
      | notify  | http | rejecting |           | 5           |
      | archive | noop |           | notify    |             |
    When I ingest an "order.created" event with idempotency key "fail-1"
    Then the execution should fail within 15 seconds
    And step "notify" should be "failed" after 1 attempt
    And step "archive" should never have been scheduled
    And the webhook "rejecting" should have received 1 delivery

  Scenario: Cloud metadata endpoints are never contacted
    Given I create a workflow "SSRF" triggered by "order.created" with steps:
      | id    | type | url                                      | maxAttempts |
      | steal | http | http://169.254.169.254/latest/meta-data/ | 3           |
    When I ingest an "order.created" event with idempotency key "ssrf-1"
    Then the execution should fail within 15 seconds
    And step "steal" should be "failed" after 1 attempt
