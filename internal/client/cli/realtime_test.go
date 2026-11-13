package cli_test

import (
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode"
	"unicode/utf8"
)

// realTime lists the tests whose outcome hangs on how fast things happen:
// each shortens a timing that what it waits for does reach, holds a window
// open for a set time, or measures how long something took
// (docs/subsystems/code-layout.md, "Running the S1 tests"). Each is written
// as a test whose name starts with a lower-case letter instead of Test, and
// does not call t.Parallel(); TestRealTime runs it beside the others. It is
// added here in alphabetical order.
var realTime = []func(*testing.T){
	aBareNameRefreshNeverMakesTheDeviceMuxReconnect,
	aBulkTransferThroughKokoExecCarriesOnAcrossADrop,
	aCommandThatEndsWhileDetachedGivesItsStatusOnTheResume,
	aCommandWaitsAtMostCommandWaitForTheDeviceConnection,
	aConnectionThatNeverReportsFailsWithinCommandWait,
	aDeviceMuxThatExitsAtOnceSendsTheCommandDirect,
	aDroppedCallDoesNotCancelARunningEnroll,
	aHalfCloseBeforeADropSurvivesIt,
	aHostWithClientAliveIntervalKeepsADetachedSSHSession,
	aLiveSSHSessionEndsWhenARelayInTheMiddleStopsAnswering,
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
	enrollRecordsNothingWhenARelayInTheMiddleChangesMeanwhile,
	enrollRecordsNothingWhenTheChainBreaksMeanwhile,
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

// realTimeAtOnce is how many realTime tests TestRealTime runs at once. They
// mostly wait, so the batch takes about as long as its longest test at
// eight, while a loaded runner still gives each the CPU its margins allow
// for: all of them at once failed under load where eight did not (#189).
const realTimeAtOnce = 8

// TestRealTime runs the realTime tests realTimeAtOnce at a time, as subtests
// named after them with a capital first letter. They do not call t.Parallel,
// whose bound is GOMAXPROCS, too few for tests that mostly wait; each runs in
// a goroutine of its own instead. TestRealTime runs no t.Parallel itself, so
// Go runs it with the package's other serial tests, before any test that
// calls t.Parallel: the realTime tests never share the CPU with the busy
// parallel tests.
func TestRealTime(t *testing.T) { //nolint:paralleltest // serial, so it runs before the parallel batch
	slots := make(chan struct{}, realTimeAtOnce)
	var wg sync.WaitGroup
	for _, test := range realTime {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			t.Run(testName(test), test)
		})
	}
	wg.Wait()
}

// testName is the name of the function test, with a capital first letter.
func testName(test func(*testing.T)) string {
	_, name, _ := strings.CutLast(runtime.FuncForPC(reflect.ValueOf(test).Pointer()).Name(), ".")
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(first)) + name[size:]
}
