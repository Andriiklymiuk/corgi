# corgi idea

## corgi idea

One page per thing to build: the agent writes it, you and others comment, it answers, then builds

### Synopsis

An idea is one page for one thing you want built, kept by the agent.

  corgi idea new "a scheduling app for a coffee shop"
      the agent (claude or codex, whichever the workspace runs) names it,
      tags it, writes the page - users, screens, data, infra, open questions,
      first tasks - puts the tasks on the board and leaves its proposals as
      comments on the page
  corgi idea open brewshift
      the page in the browser, from the dashboard's port: highlight words
      and comment; the agent answers and amends the page as you watch. The
      link has a share key, so anyone you send it to can read and comment
  corgi idea comment brewshift "use postgres" --quote "sqlite"
      the same from a terminal (or an editor); corgi idea resolve closes
  corgi idea build brewshift --workspace api
      hand it to the agents: tasks on the board, a plan that works through
      them in worktrees, the daemon starts the first

The page is markdown (corgi idea path `<slug>` prints where); edit it in any
editor, the browser follows. Comments, replies, statuses and files sit
beside it in comments.json. Nothing leaves this machine but what the
dashboard's public URL already carries.

Every new comment, answer and page change rings on the desktop and the
notifyUrl (Telegram): corgi idea notify off keeps them quiet.

### Options

```
  -h, --help   help for idea
```

### Options inherited from parent commands

```
      --describe                  Describe contents of corgi-compose file
      --dockerContext string      Specify docker context to use, can be default,orbctl,colima (default "default")
  -l, --exampleList               List examples to choose from. Click on any example to download it
  -f, --filename string           Custom filepath for for corgi-compose
      --fromScratch               Clean the .corgi/corgi_services folder before running
  -t, --fromTemplate string       Create corgi service from template url
      --fromTemplateName string   Create corgi service from template name and url
  -g, --global                    Use global path to one of the services
      --interactive               Force interactive prompts even when no TTY/agent detected
      --isolate string            Run this workspace under a named lease: own port block, own database names, own containers
      --json                      Emit machine-readable JSON output
      --privateToken string       Private token for private repositories to download files
  -o, --runOnce                   Run corgi once and exit
      --silent                    Hide all welcome messages
```

### SEE ALSO

* [corgi](corgi)	 - Corgi cli magic friend
* [corgi idea answer](corgi_idea_answer)	 - One pass of the agent over the comments waiting on it: replies, and the page amended where asked
* [corgi idea build](corgi_idea_build)	 - Hand the page to the agents: tasks on the board, a plan through them, the daemon starts building
* [corgi idea comment](corgi_idea_comment)	 - Comment on the page - on highlighted words with --quote, or on the whole page
* [corgi idea edit](corgi_idea_edit)	 - Replace the page's text with a file's, keeping the version before as a revision
* [corgi idea file](corgi_idea_file)	 - One of the page's files: its bytes, or with --json its name, type and base64 data
* [corgi idea history](corgi_idea_history)	 - The page's earlier versions - who changed it and why - or one of them in full
* [corgi idea list](corgi_idea_list)	 - Every idea page, newest change first, with what waits on whom
* [corgi idea new](corgi_idea_new)	 - The agent writes the page for a sentence: a name, tags, the spec, first tasks, proposals
* [corgi idea notify](corgi_idea_notify)	 - Whether comments, answers and page changes ring (desktop and the notifyUrl, Telegram for most)
* [corgi idea open](corgi_idea_open)	 - The page in the browser, to read, highlight and comment
* [corgi idea path](corgi_idea_path)	 - Where the page's markdown is, for an editor
* [corgi idea reopen](corgi_idea_reopen)	 - Open a comment again: the agent takes another look
* [corgi idea resolve](corgi_idea_resolve)	 - Close a comment: it is settled
* [corgi idea rm](corgi_idea_rm)	 - Remove the page, its comments and files (board tasks stay)
* [corgi idea send](corgi_idea_send)	 - Send the page out: an epic with its tickets in Jira or Linear, or a Confluence page
* [corgi idea set](corgi_idea_set)	 - Change what the page is called, tagged, typed, where it builds, whether the agent answers on its own
* [corgi idea share](corgi_idea_share)	 - The open link (anyone with it reads and comments); --off closes the page, --rotate gives it a new key
* [corgi idea show](corgi_idea_show)	 - The page and its comments, as JSON with --json or as text
* [corgi idea tickets](corgi_idea_tickets)	 - The page's split into tickets, and where each stands
* [corgi idea watch](corgi_idea_watch)	 - Keep answering: every comment that arrives on any page (or this one) gets the agent's reply

###### Auto generated by spf13/cobra on 26-Oct-2026
