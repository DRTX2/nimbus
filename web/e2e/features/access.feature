@e2e @smoke
Feature: Signing in to the dashboard
  Responders reach Nimbus from an alert link, often on a phone. The dashboard
  must send anonymous visitors to sign in, reject bad credentials clearly, and
  return people to where they were going.

  Scenario: An anonymous visitor is asked to sign in first
    When I open the dashboard without signing in
    Then I should be asked to sign in

  Scenario: A rejected credential explains what went wrong
    When I open the dashboard without signing in
    And I sign in with the credential "nmb_not-a-real-key"
    Then I should see the message "That credential was not accepted. It may be revoked, expired, or for a different Nimbus."

  Scenario: Signing in returns me to the page I asked for
    When I open the dashboard without signing in
    And I sign in with the owner credential
    Then I should see the page "Incidents"

  @mobile
  Scenario: Signing out ends the session
    Given I am signed in as the owner
    When I sign out
    Then I should be asked to sign in

  Scenario: A viewer only sees what their role allows
    Given the owner has issued a "viewer" key for "vera"
    And I am signed in as "vera"
    When I open "Workflows" from the navigation
    Then I should not see the button "Create workflow"
    And the navigation should not offer "API keys"
    And the navigation should not offer "Audit log"
