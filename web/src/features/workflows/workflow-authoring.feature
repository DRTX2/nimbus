Feature: Authoring workflows
  Definitions are checked in the browser for the mistakes people make most
  often, so broken workflows never reach the API.

  Background:
    Given I am signed in as an operator with no workflows

  Scenario: A dependency loop is rejected before submission
    When I start a workflow named "Loop"
    And I enter a definition where "a" and "b" depend on each other
    And I submit the workflow
    Then I am told "Steps depend on each other in a loop. Remove one dependency to break it."
    And no workflow was sent to the API

  Scenario: Invalid JSON is explained
    When I start a workflow named "Typo"
    And I enter the definition "{ not json"
    And I submit the workflow
    Then I am told the definition is not valid JSON

  Scenario: A valid definition is created and opened
    When I start a workflow named "Order audit"
    And I submit the workflow
    Then the API receives a workflow named "Order audit"
    And I land on the new workflow
