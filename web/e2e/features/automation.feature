@e2e @regression
Feature: Running and governing automations
  Operators build workflows that react to events, check them with a test
  event, and protect sensitive ones behind a second person's approval.

  Background:
    Given I am signed in as the owner

  Scenario: A test event runs the workflow to completion
    Given a workflow "Order audit" that starts on "order.created"
    When I open the workflow "Order audit"
    And I send a test event
    Then the run should finish as "Succeeded"
    And every step should show "Succeeded"

  Scenario: A definition with a dependency loop is caught before saving
    When I open "Workflows" from the navigation
    And I start a workflow named "Loop"
    And I break the definition with a dependency loop
    And I submit the workflow
    Then I should see the message "Steps depend on each other in a loop. Remove one dependency to break it."

  Scenario: A protected run needs someone other than the requester
    Given the owner has issued a "operator" key for "olivia"
    And a protected workflow "Refunds" that "olivia" ran on "refund.requested"
    When I open "Approvals" from the navigation
    And I approve the pending run
    Then the pending run should show "Approved"

  Scenario: The requester cannot approve their own run
    Given a protected workflow "Payouts" that "owner" ran on "payout.requested"
    When I open "Approvals" from the navigation
    Then the pending run should show "You requested this run"

  Scenario: An API key is shown once and can be revoked
    When I open "API keys" from the navigation
    And I issue a "viewer" key named "Status page"
    Then the new key should be shown once
    When I revoke the key named "Status page"
    Then the issued key should no longer work
