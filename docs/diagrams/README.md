# Diagrams

The diagrams in `docs/` are drawn in [Excalidraw](https://excalidraw.com).
Each `.excalidraw` file here is the editable source of one of them:

- `architecture.excalidraw`: the system architecture (`docs/architecture.svg`)
- `pipeline.excalidraw`: the matching funnel (`docs/pipeline.svg`)

To change one:

1. Open [excalidraw.com](https://excalidraw.com) (or the Excalidraw extension
   for VS Code) and open the `.excalidraw` file.
2. Edit it, then save it back here (File, Save to disk).
3. Export it twice with **Export image → SVG**, background on: once as
   `docs/<name>.svg`, once with **Dark mode** as `docs/<name>-dark.svg`.
   The README shows whichever matches the reader's theme.

Keep the style: one logo, a name and one technology line per box; dashed
boundaries for hosts and trust zones; short protocol labels on arrows; and a
legend for every color and line style used. Excalifont for names, Excalidraw's
own colors. Logos come from [Simple Icons](https://simpleicons.org) and
[Lucide](https://lucide.dev).
