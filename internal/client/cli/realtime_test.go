package cli_test

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// realTime lists the tests whose outcome hangs on how fast things happen:
// each shortens a timing that what it waits for does reach, holds a window
// open for a set time, or measures how long something took
// (docs/subsystems/code-layout.md, "Running the S1 tests"). Each is written
// as a test whose name starts with a lower-case letter instead of Test, and
// does not call t.Parallel(), which TestRealTime does; it is added here in
// alphabetical order.
var realTime = []func(*testing.T){
	aBareNameRefreshNeverMakesTheDeviceMuxReconnect,
	aBrowsersGraceLeavesATunnelsAsItIs,
	aBulkTransferThroughKokoExecCarriesOnAcrossADrop,
	aCommandThatEndsWhileDetachedGivesItsStatusOnTheResume,
	aCommandWaitsAtMostCommandWaitForTheDeviceConnection,
	aConnectionThatNeverReportsFailsWithinCommandWait,
	aDeviceMuxThatExitsAtOnceSendsTheCommandDirect,
	aDroppedCallDoesNotCancelARunningEnroll,
	aHalfCloseBeforeADropSurvivesIt,
	aHostWithClientAliveIntervalKeepsADetachedSSHSession,
	anOpenSSHSessionKeepsTheDeviceMuxRunning,
	anotherHomeGoesDirectBesideTheDeviceMux,
	aRestoredKokoServerKeepsItsDevicesPins,
	aResumeFromAnotherDeviceSessionIsRefused,
	aRunningKokoWebKeepsTheDeviceMuxRunning,
	aSecondDeviceMuxOfTheBuildExitsAtItsLock,
	aShellCarriesOnAcrossADrop,
	aTryToReconnectEndsSoTheNextCanFollow,
	aTunnelWaitingToResumeKeepsTheDeviceMuxAndShowsInKokoStatus,
	aWakeOrARouteChangeProbesTheDeviceConnectionAtOnce,
	aWakeWhileTheDeviceMuxReconnectsTriesAgainAtOnce,
	aWebTerminalThroughKokoWebResumesAcrossADrop,
	enrollRecordsNothingWhenTheHostChangesMeanwhile,
	enrollRecordsNothingWhenTheRelayChangesMeanwhile,
	killingTheDeviceMuxEndsNoSSHSession,
	kokoDisconnectEndsAnSSHSessionWithoutAResume,
	kokoExecSaysInOneLineWhyItCouldNotResume,
	kokoTunnelSaysInOneLineWhyItCouldNotResume,
	kokoTunnelSaysWhyInOneLineBeforeTheDeviceConnection,
	kokoTunnelSendsACloseFrameOnSIGHUPAndSIGTERM,
	statusShowsTheDeviceMux,
	theDeviceMuxIdlesOutThoughItRefreshesTheBareNames,
	theDeviceMuxIdlesOutWithNothingConnected,
	theDeviceMuxProbesItsDeviceConnectionAndReplacesADeadOne,
	theDeviceMuxReconnectsAtOnceThenBacksOffWhileACommandWaits,
	theDeviceMuxRefreshesAtItsStartAndAgainAfterADrop,
	theDeviceMuxRefreshesTheBareNames,
	theGatewayEndsTheSSHSessionOfAHostThatStopsAnswering,
	theGatewayProbesAQuietHost,
	theKokoServerProbesADeviceConnectionAndClosesADeadOne,
	theRetryOfACutCallWaitsForTheKokoServer,
}

// TestRealTime runs the realTime tests in parallel with each other, as
// subtests named after them with a capital first letter. It runs no t.Parallel
// itself, so Go runs it with the package's other serial tests, before any
// test that calls t.Parallel: the realTime tests, which mostly wait, never
// share the CPU with the busy parallel tests.
func TestRealTime(t *testing.T) { //nolint:paralleltest // serial, so it runs before the parallel batch
	for _, test := range realTime {
		t.Run(testName(test), func(t *testing.T) {
			t.Parallel()
			test(t)
		})
	}
}

// testName is the name of the function test, with a capital first letter.
func testName(test func(*testing.T)) string {
	_, name, _ := strings.CutLast(runtime.FuncForPC(reflect.ValueOf(test).Pointer()).Name(), ".")
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(first)) + name[size:]
}
