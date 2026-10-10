# Diagrams

The SVGs in `docs/` are generated from these scripts, so they stay easy to
change and review as code. After editing a script:

```sh
cd docs/diagrams
python3 backend.py ../architecture.svg
python3 pipeline.py ../pipeline.svg
```

`svgkit.py` holds the shared style: cards with icons, dashed boundaries and
labelled arrows on a light background.
