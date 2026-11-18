# The Mac's screen from a phone, iPad or another Mac

"Show my Mac on the iPad", "let the phone control this Mac", "open my other Mac", "wake the studio", "turn remote screen off":

- `corgi agent screen on|off` is the switch; off ends every session at once.
- `corgi agent screen devices` lists paired phones and peer Macs with their role; `grant "<name>" control|watch|off` changes one, `forget` makes it be asked again.
- Another Mac: pair the two as peers (`corgi agent peers invite` here, `corgi agent peers join <url> <code>` there), then on the Mac to be opened `corgi agent screen on` and `corgi agent screen grant "<this Mac>" control`. Corgi Bar › Settings › Remote screen › Your other Macs and servers opens it (Touch ID first), with Terminal and Wake beside it.
- `corgi agent screen reach nearby` keeps it to the same network. Away needs no Tailscale: direct paths first, a router port (`router off` to never ask), then the corgi tunnel.
- Corgi Bar must be running on the Mac: it captures and hosts. Screen Recording (and Accessibility for control) are granted to Corgi Bar, not to corgi.
- An agent never starts a session. It can tell the person to open the Remote tab in Corgi Agents (Open screen, Terminal, iOS Simulator).
- Pair a phone from the Mac: Corgi Bar › Settings › Remote screen › Show QR, or `corgi agent pair`.
- "The picture has black bars" → Fit in the viewer (the phone-shaped part of the display, follows the pointer); Mirror is the whole display. "Text is blurry" → Picture: Sharp.
- "Stop the agent on my Mac" → `corgi agent hands pause` (or a click on the Mac, or Pause on the phone); `corgi agent hands resume` lets it carry on; `corgi agent hands log` shows what it did.
- "Clicks, keys or the pointer do nothing from the phone" → macOS has not allowed Corgi Bar to control the Mac: System Settings › Privacy & Security › Accessibility, turn on corgi-bar (Corgi Bar › Settings › Remote screen shows a Finish setup box with the button). The Remote Desktop list in the same pane is not used and not needed.
- A shell on a Linux server or peer: `corgi agent term on` and `corgi agent term allow "<peer or phone>"` on that machine (on a Mac: Corgi Bar › Settings › Remote screen › Devices, tick Terminal), then `corgi agent term <peer>` from this one, Terminal in Corgi Bar, or Settings › Laptop › Terminal in Corgi Agents (it asks which workspace to open in, then Face ID). No ssh keys or open ports; the pairing link carries it, encrypted end to end.
- A session's terminal as live text on the phone (Corgi Agents › session › Its terminal): tmux, iTerm, Terminal.app, and VS Code terminals with the corgi VS Code extension 2.35+; keys answer prompts there.
- An agent using a Mac: `corgi_mac_windows` first (text), then `corgi_mac_look` / `corgi_mac_act` / `corgi_mac_open` with `peer` for another Mac. Needs `corgi agent hands allow <peer|here>` on the used Mac; only when the user asked to operate it. A call that says a person took the Mac means you are paused: wait a minute and try that step again, do not work around it. `corgi agent hands log` shows what agents did there.
- Wake a sleeping peer on the same network: `corgi agent wake <peer>`, or Wake in Corgi Bar and Corgi Agents. The peer must have pulsed once on corgi 2.51+, and needs "Wake for network access" on (System Settings › Battery › Options).
