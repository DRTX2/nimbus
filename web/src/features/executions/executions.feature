Feature: Browsing workflow runs
  Runs are listed newest first and older runs load on demand with the cursor
  the API returned.

  Scenario: Older runs load with the next cursor
    Given I am signed in and there are two pages of runs
    When I open the runs page
    Then I see 1 run
    When I load older runs
    Then I see 2 runs
    And the second request used the cursor from the first page
