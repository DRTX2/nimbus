Feature: Deciding protected runs
  Approvers see pending runs and can decide any run except one they requested.

  Scenario: Someone else's run can be approved
    Given I am signed in as an approver
    And a run requested by someone else is pending
    When I open the approvals page
    And I approve the run
    Then the API records an "approved" decision

  Scenario: My own run cannot be approved by me
    Given I am signed in as an approver
    And a run I requested is pending
    When I open the approvals page
    Then I see "You requested this run"
    And there is no "Approve" button
