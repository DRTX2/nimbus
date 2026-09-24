Feature: Dashboard sign-in
  Responders authenticate with an API key or access token that lives only in
  the current browser tab.

  Scenario: Anonymous visitors are sent to sign in
    Given I am not signed in
    When I open "/incidents"
    Then I see the sign-in form

  Scenario: A rejected credential explains the problem
    Given I am not signed in
    And the API rejects every credential
    When I open "/"
    And I sign in with "wrong-key"
    Then I am told "That credential was not accepted. It may be revoked, expired, or for a different Nimbus."

  Scenario: A valid credential opens the page I asked for
    Given I am not signed in
    And the API accepts the owner credential
    When I open "/incidents"
    And I sign in with "good-key"
    Then I see the heading "Incidents"
    And the credential is kept for this tab only
