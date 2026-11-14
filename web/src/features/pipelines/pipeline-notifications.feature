Feature: Pipeline notifications in the dashboard
  Responders choose which workflow runs when an incident opens and which runs
  when it recovers. Only workflows with the matching trigger are offered.

  Background:
    Given workflows exist for opened and recovered incidents
    And I am signed in as "owner"

  Scenario: Opened and recovered incidents are routed to different workflows
    When I add a test pipeline "Billing" that pages "Page on-call" and reports recovery to "All clear"
    Then the API receives a pipeline that pages "Page on-call" and reports recovery to "All clear"

  Scenario: The recovery choice offers only recovery workflows
    When I start adding a pipeline
    Then the "Recovery workflow" choices are "None, All clear"
