@security @regression
Feature: Federated sign-in through OIDC
  Teams sign in with their identity provider. Nimbus verifies the token,
  provisions a least-privilege membership on first sight, and lets owners
  manage roles without sharing API keys.

  Background:
    Given a fresh tenant with an owner

  Scenario: First sign-in provisions a read-only member
    When "grace" signs in through the identity provider
    Then my role should be "viewer"
    And the audit log should record "oidc.provision"

  Scenario: A federated viewer cannot change state
    Given "grace" signs in through the identity provider
    When I send a POST request to "/v1/workflows" with:
      """
      {"name":"w","definition":{"eventType":"e","steps":[{"id":"a","type":"noop"}]}}
      """
    Then the response status should be 403

  Scenario: A promoted member gains write access on the next sign-in
    Given "grace" signs in through the identity provider
    And the owner changes the role of "grace" to "operator"
    When "grace" signs in through the identity provider
    Then my role should be "operator"
    And I create a workflow "Federated" triggered by "sso.event" with steps:
      | id   | type |
      | work | noop |

  Scenario: A suspended member is locked out
    Given "grace" signs in through the identity provider
    And the owner suspends "grace"
    When "grace" signs in through the identity provider
    And I send a GET request to "/v1/me"
    Then the response status should be 401

  Scenario Outline: An <kind> token is rejected
    Given "mallory" presents an <kind> identity token
    When I send a GET request to "/v1/me"
    Then the response status should be 401

    Examples:
      | kind           |
      | expired        |
      | untrusted      |
      | wrong-audience |
