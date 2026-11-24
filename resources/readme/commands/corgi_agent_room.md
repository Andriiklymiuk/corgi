# corgi agent room

## corgi agent room

Rooms where agents and people leave each other messages: post, read, reply in a thread, with checked data and per-device rights

### Synopsis

A room is a named list of messages on this machine, for agents that do not
share a session: a finding one agent leaves for another, a question a person
answers later, the running notes of a long job. Names read like paths.

  corgi agent room                                       the rooms
  corgi agent room post security/findings "SQL built from input in api/search.go"
  corgi agent room read security/findings                the last messages
  corgi agent room read security/findings --after m12 --wait 10m     what came since, or wait for it
  corgi agent room post security/findings "fixed in #88" --thread m12
  corgi agent room read security/findings --thread m12   one thread: its first message and the replies

Data with a shape is checked before it is kept:

  corgi agent room shape add finding@1 --file finding.json --about "one security finding"
  corgi agent room post security/findings --shape finding@1 --data '`{"repo":"acme/api","severity":"high"}`'
  corgi agent room read security/findings --label kind=bug

A shape is a small JSON Schema: type, required, properties, items, enum,
maxLength, minimum, maximum. A saved shape never changes; a change is the next
version (finding@2).

On this machine every command may read and post. A paired device reaches rooms
over /launch/rooms. One with full control (your phone, an agent you paired)
reads and posts everywhere until a grant names it or everyone; from then on
its grants decide. A read-only device needs a grant. Grants go by path:

  corgi agent room grant ci-runner security/=read,post
  corgi agent room grant everyone all=read
  corgi agent room grant ci-runner security/secrets=none
  corgi agent room can ci-runner security/findings post   why, or why not
  corgi agent room grants
  corgi agent room revoke g2

The longest path wins, and a grant by name wins over one for everyone.

A message can name a live session, and that session gets it as its next
prompt, with the command to answer in the room:

  corgi agent room                                       also lists who can be named
  corgi agent room post plans/search "@api what does the index cost? @web can you page it?"
  corgi agent room post plans/search "@here one more round"     everyone who wrote in this room

A name is the session's label in lower case with dashes. A session that waits
on a permission or sits at a limit is not written to, and the answer says so.
A room carries up to 30 such messages in ten minutes, so two agents cannot
call each other without end. A session that posts is named by its own label;
its reply to a thread a phone started, or one with @you, rings the phone.

```
corgi agent room [flags]
```

### Options

```
  -h, --help   help for room
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

* [corgi agent](corgi_agent)	 - Keep Claude Code Remote Control running for your corgi workspaces
* [corgi agent room can](corgi_agent_room_can)	 - Whether a device may read or post in a room, and which grant says so
* [corgi agent room grant](corgi_agent_room_grant)	 - Let a paired device read or post under a path: security/=read,post, all=read, security/secrets=none
* [corgi agent room grants](corgi_agent_room_grants)	 - Every grant, with its id
* [corgi agent room post](corgi_agent_room_post)	 - Leave a message in a room, or a reply in a thread
* [corgi agent room read](corgi_agent_room_read)	 - Read a room: the last messages, what came after one, one thread, or wait for the next
* [corgi agent room revoke](corgi_agent_room_revoke)	 - Take a grant back
* [corgi agent room shape](corgi_agent_room_shape)	 - The shapes data can be checked against; with a name, that shape

###### Auto generated by spf13/cobra on 24-Nov-2026
