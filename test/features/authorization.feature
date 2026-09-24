@security @regression
Feature: Role-based access control
  Every tenant-facing operation checks the caller's role. The matrix below is
  the contract; a regression here is a security incident.

  Background:
    Given a fresh tenant with an owner
    And the owner issues an "operator" API key for "operator"
    And the owner issues an "approver" API key for "approver"
    And the owner issues a "viewer" API key for "viewer"

  Scenario Outline: <role> creating a <resource> receives <status>
    Given I am signed in as "<role>"
    When I send a POST request to "<path>" with:
      """
      <body>
      """
    Then the response status should be <status>

    Examples:
      | role     | resource | path          | body                                                                                                                                  | status |
      | operator | workflow | /v1/workflows | {"name":"w","definition":{"eventType":"e","steps":[{"id":"a","type":"noop"}]}}                                                        | 201    |
      | approver | workflow | /v1/workflows | {"name":"w","definition":{"eventType":"e","steps":[{"id":"a","type":"noop"}]}}                                                        | 403    |
      | viewer   | workflow | /v1/workflows | {"name":"w","definition":{"eventType":"e","steps":[{"id":"a","type":"noop"}]}}                                                        | 403    |
      | operator | api key  | /v1/api-keys  | {"name":"k","role":"viewer"}                                                                                                          | 403    |
      | viewer   | pipeline | /v1/pipelines | {"name":"p","mode":"fixture","region":"us-east-1","queueArn":"arn:q","lambdaArn":"arn:l","dlqArn":"arn:d"}                            | 403    |
      | operator | pipeline | /v1/pipelines | {"name":"p","mode":"fixture","region":"us-east-1","queueArn":"arn:q","lambdaArn":"arn:l","dlqArn":"arn:d"}                            | 201    |

  Scenario Outline: <role> reading <path> receives <status>
    Given I am signed in as "<role>"
    When I send a GET request to "<path>"
    Then the response status should be <status>

    Examples:
      | role     | path             | status |
      | viewer   | /v1/workflows    | 200    |
      | viewer   | /v1/incidents    | 200    |
      | viewer   | /v1/executions   | 200    |
      | viewer   | /v1/audit-events | 403    |
      | viewer   | /v1/api-keys     | 403    |
      | viewer   | /v1/members      | 403    |
      | approver | /v1/approvals    | 200    |
      | approver | /v1/pipelines    | 403    |
      | operator | /v1/audit-events | 403    |
      | owner    | /v1/audit-events | 200    |
      | owner    | /v1/members      | 200    |

  Scenario: The principal endpoint exposes the effective permissions
    Given I am signed in as "viewer"
    When I send a GET request to "/v1/me"
    Then the response status should be 200
    And the response field "role" should be "viewer"
    And the response field "permissions" should contain "incident:read"

  Scenario: A revoked API key stops working immediately
    Given the owner revokes the API key of "viewer"
    And I am signed in as "viewer"
    When I send a GET request to "/v1/workflows"
    Then the response status should be 401

  Scenario: Owners cannot demote themselves
    When I try to change my own role to "viewer"
    Then the response status should be 403
