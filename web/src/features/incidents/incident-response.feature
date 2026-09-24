Feature: Incident response in the dashboard
  The incident page points at the failing pipeline stage and offers only the
  actions the responder's role allows.

  Background:
    Given a stalled-consumer incident is open

  Scenario: The failing stages are highlighted
    Given I am signed in as "owner"
    When I open the incident
    Then the "Queue" and "Consumer" stages are implicated
    And the "Dead letters" stage is not implicated

  Scenario: An operator acknowledges the incident
    Given I am signed in as "owner"
    When I open the incident
    And I press "Acknowledge"
    Then the API receives an acknowledgement for the incident

  Scenario: A viewer can read but not act
    Given I am signed in as "viewer"
    When I open the incident
    Then I do not see the "Acknowledge" action
    And I do not see the "Resolve incident" action
    And I see "Not reviewed yet."
