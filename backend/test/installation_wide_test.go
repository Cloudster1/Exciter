//go:build integration

package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// Why a test changes something there is one of in the installation, which it puts back when it ends (#90).
const (
	linkTypes        = "changes the link types for all projects"
	defaultTheme     = "names the default theme for everyone"
	armatureLink     = "connects the installation to an Armature stub of its own"
	statorLink       = "connects the installation to the Stator stub, and sets what it offers"
	singleSignOn     = "sets up the one identity provider, its group roles and who waits, and moves the realm's people"
	installationName = "renames the installation"
	webhookEndpoints = "keeps the installation's webhooks, which hear every project"
	oauthConnections = "switches OAuth connections off for everyone"
	suiteDemo        = "adds or takes away the suite demo, of which the installation has one"
)

// keepers are the helpers that put such a setting back, with what each is for.
var keepers = map[string]string{
	"keepLinkTypes": linkTypes, "keepDefaultTheme": defaultTheme, "keepArmature": armatureLink, "keepStator": statorLink,
	"singleSignOn": singleSignOn, "keepInstallationName": installationName, "keepWebhooks": webhookEndpoints,
	"keepOAuthConnections": oauthConnections, "keepSuiteDemo": suiteDemo,
}

// installationWide is every test that changes a setting of the whole installation. The suite runs one test at a
// time, so another test never sees the change, and each puts it back, so the next starts from the same place.
var installationWide = map[string]string{
	"TestLinkTypesOverTheAPI":                                    linkTypes,
	"TestRawSQLGuardsTraceLinks":                                 linkTypes,
	"TestRawSQLKeepsLinkTypesToTheirAdministrators":              linkTypes,
	"TestATypeForAllProjectsIsTheAdministratorsAlone":            linkTypes,
	"TestRawSQLKeepsLinkTypeNamesAndScopesToTheirAdministrators": linkTypes,

	"TestThemesOverTheAPI":               defaultTheme,
	"TestRawSQLKeepsThemesToTheirPeople": defaultTheme,

	"TestAnAdministratorConnectsArmatureAndMembersTheirTokens": armatureLink,
	"TestAReadOnlyArmatureTokenIsNamedSo":                      armatureLink,
	"TestAnArmatureThatDoesNotAnswer":                          armatureLink,
	"TestAFailedCaseIsFiledAsAnIssue":                          armatureLink,
	"TestAFailureIssueIsRefusedBeforeItReachesArmature":        armatureLink,
	"TestACheckFindsARevokedToken":                             armatureLink,
	"TestRawSQLWallsArmatureRows":                              armatureLink,
	"TestRawSQLGuardsArmatureLinks":                            armatureLink,
	"TestRequirementsAreTracedToArmatureIssues":                armatureLink,
	"TestAnArmatureLinkNamesTheTokenItCameThrough":             armatureLink,
	"TestIssuesAreMadeFromRequirements":                        armatureLink,
	"TestArmatureWebhooksRefreshLinkedIssues":                  armatureLink,
	"TestAPersonFollowsTheThemeArmatureShowsThem":              armatureLink,
	"TestWhatArmatureLacksIsSaidWhereItIsNeeded":               armatureLink,
	"TestCoverageFollowsTheRulesAndTheLinks":                   armatureLink,
	"TestAQueryFollowsLinksApprovalAndCoverage":                armatureLink,

	"TestAnAdministratorKeepsWebhooksAndSeesTheSecretOnce":       webhookEndpoints,
	"TestRequirementEventsReachTheReceiverAsTheOwnerMayReadThem": webhookEndpoints,
	"TestAFailingReceiverIsRetriedThenTurnedOff":                 webhookEndpoints,
	"TestARedeliveryLeavesTheRetriesGoing":                       webhookEndpoints,
	"TestAMoveToAnotherProjectIsPostedOncePerRequirement":        webhookEndpoints,
	"TestASignInWhoseExchangeTimesOutSaysSo":                     armatureLink,
	"TestARefusedRefreshWaitsBeforeItIsAskedAgain":               armatureLink,
	"TestAPayloadIsWithheldWhatItsOwnerMayNotViewWhenItIsSent":   webhookEndpoints,
	"TestTheServerNeverPostsWebhooksIntoItsOwnNetwork":           webhookEndpoints,
	"TestRawSQLKeepsWebhooksToAdministrators":                    webhookEndpoints,
	"TestTheBootstrapConnectsArmatureOnce":                       armatureLink,
	"TestTheMatrixLaysOutTheLinks":                               armatureLink,
	"TestTheMatrixTakesItsAxesLinkTypesAndDirection":             armatureLink,
	"TestTheMatrixShowsItsGaps":                                  armatureLink,
	"TestTheMatrixFollowsLinksMadeFromItsCells":                  armatureLink,
	"TestTheMatrixLaysOutStatorPages":                            statorLink,
	"TestChainsEndInStatorPages":                                 statorLink,
	"TestChainsEndInArmatureIssues":                              armatureLink,
	"TestATraceGraphDrawsTheStatorPagesOfItsRequirements":        statorLink,
	"TestAStatorLinkNamesTheTokenItCameThrough":                  statorLink,
	"TestATraceModelIsSetAndItsChainsFollowed":                   armatureLink,
	"TestATraceGraphFollowsTheLinksUpAndDownAsFarAsAsked":        armatureLink,
	"TestATraceGraphCountsWhatTheReaderMayNotSeeAndNeverNamesIt": armatureLink,
	"TestATraceGraphMarksSuspectAndBrokenLinksGapsAndEvidence":   armatureLink,
	"TestRawSQLKeepsTraceModelsToAdministrators":                 armatureLink,
	"TestMatrixAndChainViewsAreKeptApart":                        armatureLink,
	"TestABaselineKeepsItsMatrix":                                armatureLink,
	"TestTheMatrixAndItsChainsDownloadAsWorkbooks":               armatureLink,
	"TestRawSQLKeepsCoverageRulesToAdministrators":               armatureLink,
	"TestAnAssistantReachesArmatureAsItsPerson":                  armatureLink,
	"TestAResultLinksToItsCommitInArmature":                      armatureLink,

	"TestAnAdministratorConnectsStatorAndMembersTheirTokens": statorLink,
	"TestTheConnectionKnowsWhatStatorOffers":                 statorLink,
	"TestAStatorThatDoesNotAnswer":                           statorLink,
	"TestRawSQLWallsStatorRows":                              statorLink,
	"TestRequirementsAreTracedToStatorPages":                 statorLink,

	"TestTheSuiteDemoIsBuiltAndLinked":                    armatureLink,
	"TestTheSuiteDemoIsAddedOnce":                         suiteDemo,
	"TestTheSuiteDemoIsBuiltAStepAtATime":                 suiteDemo,
	"TestRawSQLKeepsTheSuiteDemoToAdministrators":         suiteDemo,
	"TestRawSQLGuardsStatorLinks":                         statorLink,
	"TestTheBootstrapConnectsStatorOnce":                  statorLink,
	"TestRequirementsShowOnTheStatorPagesTheyAreLinkedTo": statorLink,
	"TestRawSQLGuardsStatorRemoteLinks":                   statorLink,
	"TestAnAssistantReachesStatorAsItsPerson":             statorLink,

	"TestSignInThroughKeycloak":                              singleSignOn,
	"TestPeopleAreConnectedToArmatureAndStatorAtSignIn":      singleSignOn,
	"TestASignatureAfterSigningInAgainAtTheProvider":         singleSignOn,
	"TestABaselineIsSignedAfterSigningInAgainAtTheProvider":  singleSignOn,
	"TestSignInRefusals":                                     singleSignOn,
	"TestSignInUpsertsBySubjectAndSyncsOnlyProviderGroups":   singleSignOn,
	"TestOnlyExistingGroupsAreJoinedWhenCreationIsOff":       singleSignOn,
	"TestTheProviderIsTheAdministratorsToConfigure":          singleSignOn,
	"TestGroupRolesAreRefusedWhatCannotWork":                 singleSignOn,
	"TestAMappedGroupGrantsItsRoleAndTakesItBack":            singleSignOn,
	"TestAMappedGroupLetsSomebodyInWithoutWaiting":           singleSignOn,
	"TestTheOwnerIsNeverDemotedByAMapping":                   singleSignOn,
	"TestAnAdministratorSwitchesOffASignedInMember":          singleSignOn,
	"TestANewPersonWaitsUntilAnAdministratorLetsThemIn":      singleSignOn,
	"TestTheBootstrapConfiguresTheProviderAndItsMembersOnce": singleSignOn,
	"TestTheBootstrapIsIdempotent":                           singleSignOn,
	"TestRawSQLKeepsGroupRolesToTheAdministrators":           singleSignOn,
	"TestRawSQLKeepsJoinRequestsToTheirAdministrators":       singleSignOn,
	"TestRawSQLKeepsSignInToItsAdministrators":               singleSignOn,
	"TestAGroupKeepsItsMembersInItsOrganization":             singleSignOn,
	"TestAPartnerAppExchangesAPersonsTokenFromTheRealm":      singleSignOn,
	"TestAnExchangeIsRefusedWhatTheRealmDidNotVouchFor":      singleSignOn,
	"TestAnExchangeLetsInWhomTheMappingLetsIn":               singleSignOn,

	"TestTheInstallationIsRenamedByAnAdministrator":  installationName,
	"TestWriteReportsThePositionPastItsCommit":       installationName,
	"TestReadYourWritesWhileReplayIsPaused":          installationName,
	"TestALaggingConnectionFallsBackToThePrimary":    installationName,
	"TestAReadDoesNotGoBackOnceThePrimaryAnswered":   installationName,
	"TestTheNextRequestShowsWhatThePrimaryShowed":    installationName,
	"TestALaggingReplicaLeavesTheRotationAndReturns": installationName,
	"TestAReadSeesOneMomentThroughout":               installationName,

	"TestOAuthConnectionsAreSwitchedOffAndOn": oauthConnections,
}

// The list above is what the code does: a test that reaches a keeper, itself or through a fixture, is listed with
// that keeper's reason, nothing listed has stopped, and no test in the package runs in parallel.
func TestInstallationWideTestsAreListedAndRunAlone(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("read the suite's files: %v", err)
	}
	calls := map[string]map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range files {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			called := map[string]bool{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					switch f := call.Fun.(type) {
					case *ast.Ident:
						called[f.Name] = true
					case *ast.SelectorExpr:
						called[f.Sel.Name] = true
					}
				}
				return true
			})
			if called["Parallel"] {
				t.Errorf("%s runs in parallel; every test shares the one installation, so the suite runs one at a time", fn.Name.Name)
			}
			calls[fn.Name.Name] = called
		}
	}
	// reasons follows the calls until nothing more is learned: a fixture that keeps something passes its reason on.
	reasons := map[string]map[string]bool{}
	for keeper, reason := range keepers {
		reasons[keeper] = map[string]bool{reason: true}
	}
	for learned := true; learned; {
		learned = false
		for name, called := range calls {
			for callee := range called {
				for reason := range reasons[callee] {
					if reasons[name] == nil {
						reasons[name] = map[string]bool{}
					}
					if !reasons[name][reason] {
						reasons[name][reason], learned = true, true
					}
				}
			}
		}
	}
	for name, why := range reasons {
		if len(name) < 5 || name[:4] != "Test" {
			continue
		}
		listed, ok := installationWide[name]
		if !ok || !why[listed] {
			t.Errorf("%s changes the whole installation (%v); list it in installationWide with its reason", name, why)
		}
	}
	for name := range installationWide {
		if _, ok := calls[name]; !ok {
			t.Errorf("%s is listed but no such test exists", name)
		} else if len(reasons[name]) == 0 {
			t.Errorf("%s is listed but changes nothing of the whole installation; take it off the list", name)
		}
	}
}
