package main

import "strings"

// where is what a run says of itself: the repository, the commit, the branch, the job and the run's address.
type where struct {
	repository, commit, branch, job, runURL string
}

// detect reads where a pipeline runs from the variables GitHub Actions, Gitea Actions and GitLab CI set; anything a
// pipeline does not set stays empty for a flag to fill.
func detect(env func(string) string) where {
	switch {
	case env("GITLAB_CI") != "":
		branch := env("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME")
		if branch == "" {
			branch = env("CI_COMMIT_REF_NAME")
		}
		return where{repository: env("CI_PROJECT_PATH"), commit: env("CI_COMMIT_SHA"), branch: branch, job: env("CI_JOB_NAME"),
			runURL: env("CI_JOB_URL")}
	case env("GITHUB_ACTIONS") != "" || env("GITEA_ACTIONS") != "":
		// A pull request's own ref is the merge commit's; the branch it brings is its head.
		branch := env("GITHUB_HEAD_REF")
		if branch == "" {
			branch = env("GITHUB_REF_NAME")
		}
		var run string
		if server, repo, id := env("GITHUB_SERVER_URL"), env("GITHUB_REPOSITORY"), env("GITHUB_RUN_ID"); server != "" && repo != "" && id != "" {
			run = strings.TrimRight(server, "/") + "/" + repo + "/actions/runs/" + id
		}
		return where{repository: env("GITHUB_REPOSITORY"), commit: env("GITHUB_SHA"), branch: branch, job: env("GITHUB_JOB"), runURL: run}
	}
	return where{}
}
