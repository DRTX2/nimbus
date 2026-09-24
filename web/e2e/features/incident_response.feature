@e2e @regression
Feature: Responding to a pipeline incident
  When a consumer stalls, the on-call engineer needs to see where the pipeline
  is failing, tell the team they are on it, record whether the alert was
  useful, and close it with a note for the next responder.

  Background:
    Given a test pipeline "Billing" whose consumer has stalled
    And I am signed in as the owner

  Scenario: The incident points at the failing stage
    When I open the incident for pipeline "Billing"
    Then I should see the page "Consumer stalled"
    And the consumer stage should be marked as implicated

  @mobile
  Scenario: A responder works the incident from open to resolved
    When I open the incident for pipeline "Billing"
    And I acknowledge the incident
    Then the incident status should read "Acknowledged"
    When I classify it as "Real problem"
    And I resolve it noting "Consumer redeployed with a fixed IAM policy"
    Then the incident status should read "Resolved"
    And the timeline should show "Opened, Acknowledged, Resolved"
