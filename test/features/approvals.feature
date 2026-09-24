@security @regression
Feature: Separation of duties for protected workflows
  Protected workflows pause until a different principal approves them, so no
  single credential can both request and authorize a sensitive action.

  Background:
    Given a fresh tenant with an owner
    And the owner issues an "operator" API key for "olivia"
    And the owner issues an "approver" API key for "ada"
    And I am signed in as "olivia"
    And I create a protected workflow "Refund" triggered by "refund.requested" with steps:
      | id     | type |
      | refund | noop |
    And I ingest a "refund.requested" event with idempotency key "refund-1"

  Scenario: A protected execution waits for approval
    Then the execution should await approval within 5 seconds

  Scenario: The requester cannot approve their own execution
    When "olivia" approves the pending approval
    Then the response status should be 403

  Scenario: A different approver releases the execution
    When "ada" approves the pending approval
    Then the response status should be 200
    And the execution should succeed within 15 seconds
    And the audit log should record "approval.decide"

  Scenario: A rejection cancels the execution
    When "ada" rejects the pending approval
    Then the response status should be 200
    And the execution should be cancelled within 5 seconds

  Scenario: A decided approval cannot be reversed
    Given "ada" approves the pending approval
    When "ada" rejects the pending approval
    Then the response status should be 409
