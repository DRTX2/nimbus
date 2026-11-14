@e2e @regression
Feature: Scheduled CloudWatch checks
  Workers check every active AWS pipeline once per closed five-minute window,
  so incidents open without anyone polling. Scheduled calls spend the same
  tenant CloudWatch budget as manual checks, keeping AWS cost bounded.

  Background:
    Given a fresh tenant with an owner

  Scenario: The scheduler opens incidents without manual polling
    Given CloudWatch reports stalled metrics
    And an AWS pipeline "orders" that opens incidents after 1 window
    When the CloudWatch scheduler runs
    Then a "consumer_stalled" incident should be open
    And the audit log should record "pipeline.check"

  Scenario: A pipeline is checked at most once per window
    Given CloudWatch reports healthy metrics
    And an AWS pipeline "orders"
    When the CloudWatch scheduler runs 2 times
    Then CloudWatch should have been called 1 time for the pipeline

  Scenario: Scheduled checks stop once the tenant budget is spent
    Given CloudWatch reports healthy metrics
    And an AWS pipeline "billing"
    And I check the pipeline against CloudWatch 3 times
    When the CloudWatch scheduler runs
    Then CloudWatch should have been called 0 times for the pipeline
