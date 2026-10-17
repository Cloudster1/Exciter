/** The strings of verification evidence (#200): test results from CI on a requirement and the branch that counts. */
export const evidenceEn = {
  results: {
    title: "Test results",
    none: "No test run names this requirement yet. A test names it with [verifies KEY] in its name.",
    counting: (branch: string) => `On ${branch}, which counts`,
    other: (branch: string) => `On ${branch}`,
    outcome: { passed: "Passed", failed: "Failed", skipped: "Skipped", errored: "Errored" } as Record<string, string>,
    outdated: (revision: number) => `Outdated: run against revision ${revision}`,
    outdatedHint: "The requirement was revised since. Run the test again so the result is of what it says now.",
    commit: (sha: string) => `Commit ${sha}`,
    run: "Run",
    inArmature: (issueKey: string) => `In Armature, ${issueKey}`,
    job: (job: string) => `Job ${job}`,
    when: (at: string) => `at ${at}`,
  },
  settings: {
    title: "Evidence",
    intro:
      "Test results arrive from CI per branch. The counting branch is the one whose results count for coverage, the matrix and queries; the others show on each requirement.",
    branch: "Counting branch",
    save: "Save",
    saved: "Saved.",
    readOnly: "Only the project's administrators name the counting branch.",
  },
  sources: {
    legend: "Verified from",
    links: "Links",
    tests: "Test results",
    hint: "What makes a requirement of this type verified: verifying links, passing test results on the counting branch, or both.",
  },
  matrix: {
    tests: "Tests",
    outdated: "outdated",
  },
  chains: {
    tests: "Tests",
  },
};
