package deployer

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/plotly/plotbot"
	"github.com/plotly/plotbot/testutils"
	"github.com/plotly/plotbot/util"
)

func newTestDep(dconf DeployerConfig, bot plotbot.BotLike, runner Runnable) *Deployer {

	execPath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}

	defaultdconf := DeployerConfig{
		RepositoryPath:      filepath.Dir(execPath),
		AnnounceRoom:        "#streambed",
		ProgressRoom:        "#deploy",
		DefaultBranch:       "production",
		AllowedProdBranches: []string{"master"},
	}

	if dconf.RepositoryPath != "" {
		defaultdconf.RepositoryPath = dconf.RepositoryPath
	}
	if dconf.AnnounceRoom != "" {
		defaultdconf.AnnounceRoom = dconf.AnnounceRoom
	}
	if dconf.ProgressRoom != "" {
		defaultdconf.ProgressRoom = dconf.ProgressRoom
	}
	if dconf.DefaultBranch != "" {
		defaultdconf.DefaultBranch = dconf.DefaultBranch
	}
	if len(dconf.AllowedProdBranches) != 0 {
		defaultdconf.AllowedProdBranches = dconf.AllowedProdBranches
	}

	return &Deployer{
		config:   &defaultdconf,
		bot:      bot,
		runner:   runner,
		progress: make(chan string, 1000),
	}
}

func defaultTestDep(cmdDelay time.Duration) *Deployer {
	return newTestDep(
		DeployerConfig{},
		testutils.NewDefaultMockBot(),
		&testutils.MockRunner{
			ParseVars: func(c string, s ...string) []string {
				switch c {
				case "ansible-playbook":
					return []string{
						"GO_CMD_PROCESS_OUTPUT={{ansible-output}}",
						fmt.Sprintf("GO_CMD_PROCESS_DELAY=%d", cmdDelay/time.Second),
					}
				default:
					return []string{}
				}
			},
		})
}

func captureProgress(dep *Deployer, waitTime time.Duration) (util.Searchable, error) {

	timer := time.NewTimer(waitTime)
	done := make(chan bool, 2)
	progress := util.Searchable{}
	for {
		select {
		case <-timer.C:
			return progress, fmt.Errorf("timer expired without progress")
		case <-done:
			return progress, nil
		case p := <-dep.progress:
			progress = append(progress, p)

			// if we get some progress we can assume runningJob is active
			// and if it subsequently becomes nil we can assume the job is
			// complete and we can finish waiting for progress.
			if len(progress) == 1 {
				go func() {
					ticker := time.NewTicker(time.Millisecond * 100)
					for _ = range ticker.C {
						if dep.runningJob == nil {
							ticker.Stop()
							done <- true
						}
					}
				}()
			}
		}
	}
}

func clearMocks(dep *Deployer) {
	testutils.ClearMockBot(dep.bot.(*testutils.MockBot))
	testutils.ClearMockRunner(dep.runner.(*testutils.MockRunner))
}

// This test is called by the the mock cmd.Run() or pty.Start(cmd)
func TestCmdProcess(t *testing.T) {

	if os.Getenv("GO_WANT_CMD_PROCESS") != "1" {
		return
	}

	delay := os.Getenv("GO_CMD_PROCESS_DELAY")
	i, err := strconv.Atoi(delay)
	if err == nil {
		time.Sleep(time.Second * time.Duration(i))
	}

	output := os.Getenv("GO_CMD_PROCESS_OUTPUT")
	if output != "" {
		fmt.Println(output)
	}

	exitCode := os.Getenv("GO_CMD_PROCESS_EXIT")
	i, err = strconv.Atoi(exitCode)
	if err == nil {
		os.Exit(i)
	}
}

func TestCancelDeployNotRunning(t *testing.T) {
	dep := defaultTestDep(time.Second)
	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "cancel deploy"))

	bot := dep.bot.(*testutils.MockBot)
	if len(bot.TestReplies) != 1 {
		t.Fatalf("expected 1 reply found %d", len(bot.TestReplies))
	}

	actual := bot.TestReplies[0].Text
	expected := "No deploy running, sorry friend.."
	if actual != expected {
		t.Errorf("exected '%s' but found '%s'", expected, actual)
	}
}

func TestStageDeploy(t *testing.T) {
	dep := defaultTestDep(time.Second)
	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy to stage"))

	progress, err := captureProgress(dep, time.Second*2)
	if err != nil {
		t.Fatal(err)
	}

	expectContain := util.Searchable{
		"ansible-playbook -i tools/",
		"--tags updt_streambed",
		"{{ansible-output}}",
		"terminated successfully",
	}

	if !progress.ContainsAll(expectContain...) {
		t.Errorf("expected progress %s to contain all of %s", progress.String(),
			expectContain.String())
	}

	runner := dep.runner.(*testutils.MockRunner)
	if len(runner.Jobs) != 3 {
		t.Fatalf("expected 3 job found %d", len(runner.Jobs))
	}

	if !(runner.Jobs[0].Contains("git") && runner.Jobs[1].Contains("git")) {
		t.Fatalf("expected first two jobs to be git jobs (fetch then pull)")
	}

	if !runner.Jobs[2].Contains("ansible-playbook") {
		t.Fatalf("expected last job to be ansible job")
	}

	bot := dep.bot.(*testutils.MockBot)
	if len(bot.TestReplies) != 2 {
		t.Fatalf("expected 2 replies found %d", len(bot.TestReplies))
	}

	actual := bot.TestReplies[0].Text
	expected := fmt.Sprintf("<@%s> deploying", testutils.DefaultFromUser)
	if !strings.Contains(actual, expected) {
		t.Errorf("exected '%s' to contain '%s'", expected, actual)
	}

	actual = bot.TestReplies[1].Text
	expected = fmt.Sprintf("<@%s> your deploy was successful", testutils.DefaultFromUser)
	if actual != expected {
		t.Errorf("exected '%s' but found '%s'", expected, actual)
	}
}

func TestProdDeployWithTags(t *testing.T) {
	dep := defaultTestDep(time.Second)
	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy to prod with tags: umwelt"))

	progress, err := captureProgress(dep, time.Second*2)
	if err != nil {
		t.Fatal(err)
	}

	expectContain := util.Searchable{"ansible-playbook -i tools/",
		"--tags umwelt",
		"{{ansible-output}}",
		"terminated successfully",
	}

	if !progress.ContainsAll(expectContain...) {
		t.Errorf("expected progress %s to contain all of %s", progress.String(),
			expectContain.String())
	}

	bot := dep.bot.(*testutils.MockBot)
	if len(bot.TestReplies) != 2 {
		t.Fatalf("expected 2 replies found %d", len(bot.TestReplies))
	}

	actual := bot.TestReplies[0].Text
	expected := fmt.Sprintf("<@%s> deploying", testutils.DefaultFromUser)
	if !strings.Contains(actual, expected) {
		t.Errorf("exected '%s' to contain '%s'", expected, actual)
	}

	actual = bot.TestReplies[1].Text
	expected = fmt.Sprintf("<@%s> your deploy was successful", testutils.DefaultFromUser)
	if actual != expected {
		t.Errorf("exected '%s' but found '%s'", expected, actual)
	}
}

func TestLockUnlock(t *testing.T) {

	// First test locking - set command delay to 0 so we can wait for progress
	// on a shorter interval.
	dep := defaultTestDep(time.Second * 0)
	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "please lock deployment"))

	// there should be no progress
	_, err := captureProgress(dep, time.Millisecond*500)
	if err == nil {
		t.Errorf("expected timeout error while capturing non-existent progress")
	}

	runner := dep.runner.(*testutils.MockRunner)
	if len(runner.Jobs) != 0 {
		t.Fatalf("expected no job to be run found %d", len(runner.Jobs))
	}

	bot := dep.bot.(*testutils.MockBot)
	if len(bot.TestReplies) != 1 {
		t.Fatalf("expected 1 replies found %d", len(bot.TestReplies))
	}

	actual := bot.TestReplies[0].Text
	expected := "Deployment is now locked"
	if !strings.Contains(actual, expected) {
		t.Fatalf("exected '%s' to contain '%s'", expected, actual)
	}

	// Then make sure a deploy fails while locked
	clearMocks(dep)
	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsgFromUser(dep.bot, "deploy to prod", "rodoh"))

	_, err = captureProgress(dep, time.Millisecond*500)
	if err == nil {
		t.Errorf("expected timeout error while capturing non-existent progress")
	}

	if len(runner.Jobs) != 0 {
		t.Fatalf("expected no job to be run found %d", len(runner.Jobs))
	}

	if len(bot.TestReplies) != 1 {
		t.Fatalf("expected 1 replies found %d", len(bot.TestReplies))
	}

	actual = bot.TestReplies[0].Text
	expected = fmt.Sprintf("Deployment was locked by %s", testutils.DefaultFromUser)
	if !strings.Contains(actual, expected) {
		t.Fatalf("exected '%s' to contain '%s'", expected, actual)
	}

	// Unlock deployment
	clearMocks(dep)
	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "unlock deployment"))

	_, err = captureProgress(dep, time.Millisecond*500)
	if err == nil {
		t.Errorf("expected timeout error while capturing non-existent progress")
	}

	if len(runner.Jobs) != 0 {
		t.Fatalf("expected no job to be run found %d", len(runner.Jobs))
	}

	if len(bot.TestReplies) != 1 {
		t.Fatalf("expected 1 replies found %d", len(bot.TestReplies))
	}

	actual = bot.TestReplies[0].Text
	expected = "Deployment is now unlocked"
	if !strings.Contains(actual, expected) {
		t.Fatalf("exected '%s' to contain '%s'", expected, actual)
	}

	// Finally make sure we can now deploy
	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy to prod"))
	captureProgress(dep, time.Millisecond*500)

	if len(runner.Jobs) != 3 {
		t.Fatalf("expected 3 job found %d", len(runner.Jobs))
	}
}

func TestCancelDeploy(t *testing.T) {

	// set up for long running deploy
	dep := defaultTestDep(time.Second * 5)

	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy to stage"))

	time.Sleep(time.Millisecond * 500)

	fromUser := "rodoh"
	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsgFromUser(dep.bot, "cancel deploy", fromUser))

	progress, err := captureProgress(dep, time.Second*4)
	if err != nil {
		t.Fatal(err)
	}

	expectContain := util.Searchable{
		"ansible-playbook",
		"--tags updt_streambed",
		"terminated with error: signal: interrupt",
	}
	if !progress.ContainsAll(expectContain...) {
		t.Errorf("expected progress %s to contain all of %s", progress.String(),
			expectContain.String())
	}

	expectNotToContain := util.Searchable{
		"terminated successfully",
		"{{ansible-output}}",
	}
	if progress.ContainsAny(expectNotToContain...) {
		t.Errorf("expected progress %s not to contain any of %s", progress.String(),
			expectContain.String())
	}

	// 3 jobs should have run
	runner := dep.runner.(*testutils.MockRunner)
	if len(runner.Jobs) != 3 {
		t.Fatalf("expected 3 job found %d", len(runner.Jobs))
	}

	// should have made 3 replies
	bot := dep.bot.(*testutils.MockBot)
	if len(bot.TestReplies) != 3 {
		t.Fatalf("expected 3 replies found %d", len(bot.TestReplies))
	}

	actual := bot.TestReplies[1].Text
	expected := "deploy: Sending Interrupt signal"
	if !strings.Contains(actual, expected) {
		t.Errorf("exected '%s' to contain '%s'", actual, expected)
	}

	actual = bot.TestReplies[2].Text
	expected = fmt.Sprintf("<@%s> your deploy failed: signal: interrupt",
		testutils.DefaultFromUser)
	if !strings.Contains(actual, expected) {
		t.Errorf("exected '%s' to contain '%s'", actual, expected)
	}
}

func TestJobAlreadyRunning(t *testing.T) {
	dep := defaultTestDep(time.Second)

	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy to stage"))

	time.Sleep(time.Millisecond * 200)

	fromUser := "rodoh"
	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsgFromUser(dep.bot, "deploy to prod", fromUser))

	_, err := captureProgress(dep, time.Second*2)
	if err != nil {
		t.Fatal(err)
	}

	bot := dep.bot.(*testutils.MockBot)
	replies := bot.TestReplies
	if len(replies) != 3 {
		t.Fatalf("expected 3 replies got %d", len(replies))
	}

	actual := replies[1].Text
	expected := "Deploy currently running"
	if !(strings.Contains(actual, fromUser) && strings.Contains(actual, expected)) {
		t.Errorf("expected reply '%s' to contain '%s' and '%s'", actual, fromUser, expected)
	}

	actual = replies[2].Text
	expected = "deploy was successful"
	if !strings.Contains(actual, expected) {
		t.Errorf("expected reply '%s' to contain '%s'", actual, expected)
	}
}

func TestHelp(t *testing.T) {
	dep := defaultTestDep(time.Second)

	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy whats up?"))

	bot := dep.bot.(*testutils.MockBot)
	replies := bot.TestReplies

	if len(replies) != 1 {
		t.Fatalf("expected 1 replies got %d", len(replies))
	}

	actual := replies[0].Text
	if !strings.Contains(strings.ToLower(actual), "usage") {
		t.Errorf("expected reply '%s' to contain '%s'", actual, "usage")
	}
	if !strings.Contains(strings.ToLower(actual), "examples") {
		t.Errorf("expected reply '%s' to contain '%s'", actual, "examples")
	}
}

func TestAllowedProdBranches(t *testing.T) {
	dep := defaultTestDep(time.Second * 0)

	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy cats to prod"))

	_, err := captureProgress(dep, time.Millisecond*500)
	if err != nil {
		t.Fatal(err)
	}

	bot := dep.bot.(*testutils.MockBot)
	replies := bot.TestReplies

	if len(replies) != 1 {
		t.Fatalf("expected 1 replies got %d", len(replies))
	}

	actual := replies[0].Text
	expected := "cats is not a legal branch for prod"
	if !strings.Contains(actual, expected) {
		t.Errorf("expected reply '%s' to contain '%s'", actual, expected)
	}

	clearMocks(dep)
	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy master to prod"))

	_, err = captureProgress(dep, time.Millisecond*500)
	if err != nil {
		t.Fatal(err)
	}

	bot = dep.bot.(*testutils.MockBot)
	replies = bot.TestReplies

	if len(replies) != 2 {
		t.Fatalf("expected 2 replies got %d", len(replies))
	}

	actual = replies[1].Text
	expected = "your deploy was successful"
	if !strings.Contains(actual, expected) {
		t.Errorf("expected reply '%s' to contain '%s'", actual, expected)
	}
}

func TestFailedGitFetch(t *testing.T) {
	dep := newTestDep(
		DeployerConfig{},
		testutils.NewDefaultMockBot(),
		&testutils.MockRunner{
			ParseVars: func(c string, s ...string) []string {
				args := util.Searchable(s)
				if c == "git" && args.Contains("fetch") {
					return []string{"GO_CMD_PROCESS_EXIT=99"}
				}
				return []string{}
			},
		})

	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy to prod"))

	_, err := captureProgress(dep, time.Millisecond*500)
	if err != nil {
		t.Fatal(err)
	}

	bot := dep.bot.(*testutils.MockBot)
	replies := bot.TestReplies

	if len(replies) != 1 {
		t.Fatalf("expected 1 reply got %d", len(replies))
	}

	actual := replies[0].Text
	expected := "Unable to pull from repo: Error executing git fetch: exit status 99"
	if !strings.Contains(actual, expected) {
		t.Errorf("expected reply '%s' to contain '%s'", actual, expected)
	}
}

func TestFailedGitCheckout(t *testing.T) {
	dep := newTestDep(
		DeployerConfig{},
		testutils.NewDefaultMockBot(),
		&testutils.MockRunner{
			ParseVars: func(c string, s ...string) []string {
				args := util.Searchable(s)
				if c == "git" && args.Contains("checkout") {
					return []string{"GO_CMD_PROCESS_EXIT=99"}
				}
				return []string{}
			},
		})

	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy to prod"))

	_, err := captureProgress(dep, time.Millisecond*500)
	if err != nil {
		t.Fatal(err)
	}

	bot := dep.bot.(*testutils.MockBot)
	replies := bot.TestReplies

	if len(replies) != 1 {
		t.Fatalf("expected 1 reply got %d", len(replies))
	}

	actual := replies[0].Text
	expected := "Unable to pull from repo: exit status 99"
	if !strings.Contains(actual, expected) {
		t.Errorf("expected reply '%s' to contain '%s'", actual, expected)
	}
}

func TestFailedAnsible(t *testing.T) {
	dep := newTestDep(
		DeployerConfig{},
		testutils.NewDefaultMockBot(),
		&testutils.MockRunner{
			ParseVars: func(c string, s ...string) []string {
				if c == "ansible-playbook" {
					return []string{"GO_CMD_PROCESS_EXIT=99"}
				}
				return []string{}
			},
		})

	dep.ChatHandler(&plotbot.Conversation{Bot: dep.bot},
		testutils.ToBotMsg(dep.bot, "deploy to prod with tags: onions"))

	progress, err := captureProgress(dep, time.Millisecond*500)
	if err != nil {
		t.Fatal(err)
	}

	if !progress.Contains("terminated") {
		t.Errorf("expected progress %s to contain 'terminated'", progress)
	}

	bot := dep.bot.(*testutils.MockBot)
	replies := bot.TestReplies

	if len(replies) != 2 {
		t.Fatalf("expected 2 replies got %d", len(replies))
	}

	actual := replies[1].Text
	expected := "your deploy failed: exit status 99"
	if !strings.Contains(actual, expected) {
		t.Errorf("expected reply '%s' to contain '%s'", actual, expected)
	}
}
