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

Keep the style: Excalifont for text, Excalidraw's own colors, dashed
boundaries for hosts and trust zones, and short labels.
