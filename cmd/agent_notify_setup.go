package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"

	"github.com/spf13/cobra"
)

var agentNotifyCmd = &cobra.Command{
	Use:     "notify",
	Aliases: []string{"notifications"},
	Short:   "Where notifications go when you are away from this machine",
	Long: `Without this, every notification stops at this machine — the desk you were
trying to leave. Point it at something your phone receives.

  corgi agent notify telegram --token <TOKEN>   set up a Telegram bot end to end
  corgi agent notify set <webhook-url>          a Slack, Discord or ntfy URL
  corgi agent notify show                       what is configured (masked)
  corgi agent notify test                       send one now`,
}

var agentNotifyTelegramCmd = &cobra.Command{
	Use:   "telegram",
	Short: "Set up Telegram notifications, resolving the chat id for you",
	Long: `Checks the token, waits for you to message the bot, reads the chat id from that
message, writes notifyUrl and sends a test.

Get a token first: message @BotFather in Telegram, send /newbot, follow it. The
token is a credential — pass it here, do not paste it into a chat or a commit.`,
	Example: `corgi agent notify telegram --token 123456:AA...`,
	Run:     runAgentNotifyTelegram,
}

var agentNotifySetCmd = &cobra.Command{
	Use:   "set <url>",
	Short: "Point notifications at a Slack, Discord or ntfy webhook",
	Args:  cobra.ExactArgs(1),
	Run:   runAgentNotifySet,
}

var agentNotifyShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print where notifications go, with the secret masked",
	Run:   runAgentNotifyShow,
}

var agentNotifyTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Send one notification to the configured destination",
	Run:   runAgentNotifyTest,
}

func init() {
	agentNotifyTelegramCmd.Flags().String("token", "", "Bot token from @BotFather")
	agentNotifyTelegramCmd.Flags().String("chat-id", "", "Skip the wait and use this chat id")
	agentNotifyTelegramCmd.Flags().Duration("wait", 3*time.Minute, "How long to wait for your message to the bot")
	agentNotifyCmd.AddCommand(agentNotifyTelegramCmd, agentNotifySetCmd, agentNotifyShowCmd, agentNotifyTestCmd)
	agentCmd.AddCommand(agentNotifyCmd)
}

func runAgentNotifyTelegram(cmd *cobra.Command, _ []string) {
	token := strings.TrimSpace(mustFlagString(cmd, "token"))
	if token == "" {
		exitWithError(utils.ErrUsage, fmt.Errorf(
			"--token is required; message @BotFather in Telegram and send /newbot to get one"), 2)
		return
	}

	bot, err := telegramGetMe(token)
	if err != nil {
		exitWithError("agent_notify", fmt.Errorf("that token was refused by Telegram: %v", err), 1)
		return
	}
	utils.Infof("✓ token belongs to @%s\n", bot)

	chatID := strings.TrimSpace(mustFlagString(cmd, "chat-id"))
	if chatID == "" {
		wait, _ := cmd.Flags().GetDuration("wait")
		chatID, err = telegramAwaitChatID(token, bot, wait)
		if err != nil {
			exitWithError("agent_notify", err, 1)
			return
		}
	}
	utils.Infof("✓ chat id %s\n", chatID)

	target := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?chat_id=%s", token, chatID)
	finishNotifySetup(target)
}

func runAgentNotifySet(_ *cobra.Command, args []string) {
	raw := strings.TrimSpace(args[0])
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		exitWithError(utils.ErrUsage, fmt.Errorf("%q is not an http(s) URL", raw), 2)
		return
	}
	utils.Infof("✓ %s webhook\n", webhookShapeFor(u.Host))
	finishNotifySetup(u.String())
}

func finishNotifySetup(target string) {
	path := agentUserConfigPath(agentDirOrEmpty())
	if err := writeNotifyURL(path, target); err != nil {
		exitWithError("agent_notify", err, 1)
		return
	}
	utils.Infof("✓ saved to %s\n", path)

	if err := sendNotifyTest(target); err != nil {
		utils.Infof("✖ test message failed: %v\n", err)
		utils.Info("  the URL is saved; fix it and run `corgi agent notify test`")
		exitProcess(1)
		return
	}
	utils.Info("✓ test message sent — check your phone")
	utils.Info("")
	utils.Info("run `corgi agent restart` so the running daemon picks it up")
}

func runAgentNotifyShow(_ *cobra.Command, _ []string) {
	target, path := configuredNotifyURL()
	if target == "" {
		utils.Infof("no notifyUrl set in %s\n", path)
		utils.Info("notifications reach this machine only — `corgi agent notify telegram --token <TOKEN>`")
		return
	}
	u, err := url.Parse(target)
	if err != nil {
		utils.Infof("notifyUrl in %s is not a usable URL\n", path)
		exitProcess(1)
		return
	}
	utils.Infof("%s → %s\n", webhookShapeFor(u.Host), maskNotifyURL(target))
	utils.Infof("in %s\n", path)
}

func runAgentNotifyTest(_ *cobra.Command, _ []string) {
	target, path := configuredNotifyURL()
	if target == "" {
		exitWithError("agent_notify", fmt.Errorf("no notifyUrl set in %s", path), 1)
		return
	}
	if err := sendNotifyTest(target); err != nil {
		exitWithError("agent_notify", err, 1)
		return
	}
	utils.Info("✓ sent — check your phone")
}

func configuredNotifyURL() (target, path string) {
	path = agentUserConfigPath(agentDirOrEmpty())
	user, err := config.LoadUser(path)
	if err != nil || user == nil {
		return "", path
	}
	return strings.TrimSpace(user.NotifyUrl), path
}

func sendNotifyTest(target string) error {
	req, err := buildNotifyRequest(webhookShapeFor(hostOf(target)), target,
		"corgi agent", "notifications are working", launcherURL())
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("the destination answered %s", resp.Status)
	}
	return nil
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

var notifyURLLine = regexp.MustCompile(`(?m)^notifyUrl:.*$`)

func writeNotifyURL(path, target string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %v", path, err)
	}
	line := fmt.Sprintf("notifyUrl: %q", target)
	body := string(data)
	if notifyURLLine.MatchString(body) {
		body = notifyURLLine.ReplaceAllString(body, line)
	} else {
		body = strings.TrimRight(body, "\n") + "\n" + line + "\n"
	}
	return os.WriteFile(path, []byte(body), 0o600)
}

var telegramTokenInURL = regexp.MustCompile(`/bot[^/]+/`)

func maskNotifyURL(raw string) string {
	if telegramTokenInURL.MatchString(raw) {
		return telegramTokenInURL.ReplaceAllString(raw, "/bot***/")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "(unreadable)"
	}
	parts := strings.Split(u.Path, "/")
	for i, part := range parts {
		if len(part) >= 12 {
			parts[i] = "***"
		}
	}
	return u.Scheme + "://" + u.Host + strings.Join(parts, "/")
}

func mustFlagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

type telegramMe struct {
	OK     bool `json:"ok"`
	Result struct {
		Username string `json:"username"`
	} `json:"result"`
	Description string `json:"description"`
}

func telegramGetMe(token string) (string, error) {
	var me telegramMe
	if err := telegramCall(token, "getMe", &me); err != nil {
		return "", err
	}
	if !me.OK || me.Result.Username == "" {
		return "", fmt.Errorf("%s", firstNonEmpty(me.Description, "no bot behind that token"))
	}
	return me.Result.Username, nil
}

type telegramUpdates struct {
	OK     bool `json:"ok"`
	Result []struct {
		Message struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"result"`
	Description string `json:"description"`
}

func telegramAwaitChatID(token, bot string, wait time.Duration) (string, error) {
	if id := telegramLatestChatID(token); id != "" {
		return id, nil
	}
	utils.Infof("\nopen https://t.me/%s in Telegram and send it any message\n", bot)
	utils.Info("(a bot cannot start the conversation, so this step is on you)")
	utils.Infof("waiting up to %s…\n", wait)

	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		if id := telegramLatestChatID(token); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("no message arrived; send one to @%s and re-run, or pass --chat-id", bot)
}

func telegramLatestChatID(token string) string {
	var updates telegramUpdates
	if err := telegramCall(token, "getUpdates", &updates); err != nil || !updates.OK {
		return ""
	}
	for i := len(updates.Result) - 1; i >= 0; i-- {
		if id := updates.Result[i].Message.Chat.ID; id != 0 {
			return fmt.Sprint(id)
		}
	}
	return ""
}

func telegramCall(token, method string, out any) error {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://api.telegram.org/bot%s/%s", token, method))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
