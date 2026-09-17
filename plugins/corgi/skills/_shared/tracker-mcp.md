# Tracker MCP detection (Linear vs Jira)

Detect once per run, then use one namespace.

- **Linear**: a `linear.app` URL, or a key whose team lives in a Linear workspace →
  `mcp__linear-server__*`.
- **Jira**: an `atlassian.net` URL or a Jira project key → `mcp__atlassian__*`
  (`getAccessibleAtlassianResources` lists the sites).
- Bare key with both connected → ask which. Neither connected → name what to connect,
  keep going with what git alone can tell, and never silently skip a write.

Common calls (tool names without their namespace prefix):

| Job                    | Linear                                                 | Jira                                                   |
| ---------------------- | ------------------------------------------------------ | ------------------------------------------------------ |
| read issue + comments  | `get_issue`                                            | `getJiraIssue`                                         |
| create issue           | `save_issue` with `title` + `team`, no `id`            | `createJiraIssue`                                      |
| comment                | `save_comment({ issueId, body })`; pass `id` to update | `addCommentToJiraIssue`                                |
| statuses / transitions | `list_issue_statuses`                                  | `getTransitionsForJiraIssue`                           |
| assign to me           | `assignee: "me"` (also id, name, email)                | `editJiraIssue`; current user from `atlassianUserInfo` |
| attachments            | read: image URLs in the body; write: `prepare_attachment_upload` → PUT bytes → `![](assetUrl)` (`forge-commands.md` §6a) | `fetch` returns ARIs and metadata, never bytes; write via the issue attachments endpoint |

Writes stay behind the calling skill's confirm gate.
