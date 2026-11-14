@e2e @regression
Feature: Routing incident recovery to a workflow
  When a stalled pipeline recovers, the team wants the all-clear sent by the
  workflow they picked when they added the pipeline, without writing code.

  Background:
    Given a workflow "All clear" that starts on "nimbus.incident.recovered"
    And I am signed in as the owner

  Scenario: A recovered incident starts the recovery workflow chosen in the dashboard
    When I add a test pipeline "Payments" that reports recovery to "All clear"
    And pipeline "Payments" stalls and then recovers
    Then the runs list should show a run of workflow "All clear"
