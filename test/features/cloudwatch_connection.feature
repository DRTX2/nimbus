@regression
Feature: CloudWatch connection validation
  A failed check must say what to fix, without revealing credentials, so the
  team can repair the connection instead of guessing.

  Scenario: A check with an unconfigured external ID explains the failure
    Given a fresh tenant with an owner
    And an AWS pipeline "billing"
    When I check the pipeline against CloudWatch 1 time
    Then the response status should be 502
    And the response field "detail" should contain "configuration"
