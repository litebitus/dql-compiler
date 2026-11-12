package promqlbackend

import (
	"testing"

	"github.com/litebitus/dql-compiler/dqlsyntax"
)

const (
	mockByJob       = `(__name__:mock_requests | sum() by job)`
	mockByJobStatus = `(__name__:mock_requests | sum() by job, status)`
	mockByJobEnv    = `(__name__:mock_requests | sum() by job, env)`
	mockUngrouped   = `(__name__:mock_requests | sum())`
	mockRateAlone   = `(__name__:mock_requests | rate(5m))`

	sumByJob       = `(sum by (job) (mock_requests))`
	sumByJobStatus = `(sum by (job, status) (mock_requests))`
	sumUngrouped   = `(sum(mock_requests))`
)

func compiledArithmetic(t *testing.T, text string) (string, error) {
	t.Helper()
	parsed, err := dqlsyntax.ParseQuery(text, "1.1")
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return CompileQuery(parsed, BackendConfig{})
}

func TestASideGroupingByMoreFieldsPairsItsSeriesWithTheOtherSidesOne(t *testing.T) {
	for text, want := range map[string]string{
		mockByJobStatus + ` / ` + mockByJob:     sumByJobStatus + ` / on (job) group_left () ` + sumByJob,
		mockByJob + ` * ` + mockByJobStatus:     sumByJob + ` * on (job) group_right () ` + sumByJobStatus,
		mockByJob + ` / ` + mockUngrouped:       sumByJob + ` / on () group_left () ` + sumUngrouped,
		mockUngrouped + ` - ` + mockByJobStatus: sumUngrouped + ` - on () group_right () ` + sumByJobStatus,
		`(__name__:mock_expiry | min() by host) * (__name__:mock_info | max() by host, owner) / 86400 - now() / 86400`: `(min by (host) (mock_expiry))` +
			` * on (host) group_right () (max by (host, owner) (mock_info)) / 86400 - time() / 86400`,
	} {
		got, err := compiledArithmetic(t, text)
		if err != nil || got != want {
			t.Errorf("%q:\n got  %s (%v)\n want %s", text, got, err, want)
		}
	}
}

func TestSidesPairAsPromQLReadsTheExpression(t *testing.T) {
	for text, want := range map[string]string{
		mockByJobStatus + ` * 100 / ` + mockByJob: sumByJobStatus + ` * 100 / on (job) group_left () ` + sumByJob,
		mockByJob + ` * 100 / ` + mockByJob:       sumByJob + ` * 100 / on (job) ` + sumByJob,
		mockByJob + ` + ` + mockByJobStatus + ` * ` + mockByJob: sumByJob + ` + on (job) group_right () ` + sumByJobStatus +
			` * on (job) group_left () ` + sumByJob,
		mockByJobStatus + ` / ` + mockByJob + ` / ` + mockByJobStatus: sumByJobStatus + ` / on (job) group_left () ` + sumByJob +
			` / on (job, status) ` + sumByJobStatus,
		mockByJob + ` - 1 - ` + mockUngrouped: sumByJob + ` - 1 - on () group_left () ` + sumUngrouped,
	} {
		got, err := compiledArithmetic(t, text)
		if err != nil || got != want {
			t.Errorf("%q:\n got  %s (%v)\n want %s", text, got, err, want)
		}
	}
}

func TestSidesGroupingAlikeOrWithoutAStatisticKeepTheirPairing(t *testing.T) {
	for text, want := range map[string]string{
		mockByJob + ` / ` + mockByJob:         sumByJob + ` / on (job) ` + sumByJob,
		mockUngrouped + ` / ` + mockUngrouped: sumUngrouped + ` / ` + sumUngrouped,
		mockRateAlone + ` / ` + mockByJob:     `(rate(mock_requests[5m])) / ` + sumByJob,
		mockByJob + ` / ` + mockRateAlone:     sumByJob + ` / (rate(mock_requests[5m]))`,
		`(__name__:mock_requests | sum() by job, status) / (__name__:mock_requests | sum() by status, job)`: sumByJobStatus +
			` / on (job, status) (sum by (status, job) (mock_requests))`,
	} {
		got, err := compiledArithmetic(t, text)
		if err != nil || got != want {
			t.Errorf("%q:\n got  %s (%v)\n want %s", text, got, err, want)
		}
	}
}

func TestSidesEachGroupingByAFieldTheOtherLacksAreRefused(t *testing.T) {
	want := "arithmetic between queries grouped by (job, status) and by (job, env) does not compile to PromQL: " +
		"PromQL pairs many series with one, so one side groups by every field of the other"
	for _, text := range []string{
		mockByJobStatus + ` / ` + mockByJobEnv,
		mockByJob + ` + ` + mockByJobStatus + ` / ` + mockByJobEnv,
	} {
		if _, err := compiledArithmetic(t, text); err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", text, err, want)
		}
	}
}
