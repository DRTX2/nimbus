@e2e @regression
Feature: Explainable pipeline incidents
  Nimbus watches SQS -> Lambda -> DLQ pipelines. Sustained symptoms open one
  deduplicated incident per detector; responders acknowledge, classify, and
  resolve it while the timeline keeps the evidence.

  Background:
    Given a fresh tenant with an owner

  Scenario Outline: A sustained <symptom> condition opens a <detector> incident
    Given a fixture pipeline "billing"
    When the pipeline reports <symptom> metrics for the windows starting 15 and 10 minutes ago
    Then a "<detector>" incident should be open

    Examples:
      | symptom        | detector         |
      | stalled        | consumer_stalled |
      | dead-lettering | dlq_growth       |
      | throttled      | lambda_throttled |
      | retry-storming | retry_storm      |

  Scenario: A single degraded window is not enough to page anyone
    Given a fixture pipeline "billing"
    When the pipeline reports stalled metrics for the window starting 10 minutes ago
    Then no incident should be open

  Scenario: The incident lifecycle is recorded end to end
    Given a fixture pipeline "billing"
    And the pipeline reports stalled metrics for the windows starting 20 and 15 minutes ago
    And a "consumer_stalled" incident should be open
    When I acknowledge the incident
    Then the response status should be 200
    And a "consumer_stalled" incident should be acknowledged
    When the pipeline reports healthy metrics for the windows starting 10 and 5 minutes ago
    Then a "consumer_stalled" incident should be recovered
    When I classify the incident as "true_positive"
    Then the response status should be 200
    When I resolve the incident noting "consumer redeployed"
    Then the response status should be 200
    And a "consumer_stalled" incident should be resolved
    And the incident timeline should read "opened, acknowledged, recovered, resolved"
    And the audit log should record "incident.resolve"

  Scenario: An opened incident dispatches the response workflow
    Given I create a workflow "Page on-call" triggered by "nimbus.incident.opened" with steps:
      | id   | type |
      | page | noop |
    And a fixture pipeline "orders" that dispatches the workflow "Page on-call"
    When the pipeline reports dead-lettering metrics for the windows starting 15 and 10 minutes ago
    Then a "dlq_growth" incident should be open
    And the workflow "Page on-call" should have 1 execution within 15 seconds

  Scenario: A recovered incident notifies responders with actionable context
    Given the webhook "on-call" responds with statuses "204"
    And I create a workflow "All clear" triggered by "nimbus.incident.recovered" with steps:
      | id     | type | webhook |
      | notify | http | on-call |
    And a fixture pipeline "orders" that dispatches the workflow "All clear" on recovery
    And the pipeline reports stalled metrics for the windows starting 20 and 15 minutes ago
    When the pipeline reports healthy metrics for the windows starting 10 and 5 minutes ago
    Then a "consumer_stalled" incident should be recovered
    And the webhook "on-call" should receive a "recovered" notification for "consumer_stalled" on pipeline "orders" within 15 seconds

  Scenario: Incidents cannot be routed to a workflow for another event
    Given I create a workflow "Order audit" triggered by "order.created" with steps:
      | id    | type |
      | audit | noop |
    When I try to add a fixture pipeline "orders" that dispatches the workflow "Order audit" on recovery
    Then the response status should be 400

  Scenario: Weekly quality reporting counts reviewed incidents
    Given a fixture pipeline "billing"
    And the pipeline reports dead-lettering metrics for the windows starting 15 and 10 minutes ago
    And a "dlq_growth" incident should be open
    And I classify the incident as "false_positive"
    When I send a GET request to "/v1/reports/incident-quality/weekly"
    Then the response status should be 200
    And the response field "items.0.falsePositives" should be "1"
