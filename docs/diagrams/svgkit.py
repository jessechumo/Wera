"""Tiny SVG kit for architecture diagrams: cards with icons, dashed
boundaries, and labelled arrows, on a light background."""
from xml.sax.saxutils import escape

FONT = "-apple-system, 'Segoe UI', Helvetica, Arial, sans-serif"
MONO = "'SFMono-Regular', Menlo, Consolas, monospace"
INK, MUTED, FAINT = "#1F2328", "#57606A", "#8C959F"
BG, CARD, LINE = "#FBFAF7", "#FFFFFF", "#D8DEE4"
C = {  # accent per component kind
    "ember": "#D9572B", "blue": "#2F6FEB", "green": "#1A7F37", "violet": "#7C3AED",
    "teal": "#0E7C86", "amber": "#B76E00", "slate": "#57606A",
}

def tint(hex_, a):
    return f"{hex_}{int(a * 255):02X}"

# 24x24 line icons (Lucide-style paths), drawn with currentColor.
ICONS = {
    "browser": '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18"/><circle cx="6.5" cy="6.5" r=".6"/><circle cx="9" cy="6.5" r=".6"/>',
    "server": '<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01M11 7.5h6M11 16.5h6"/>',
    "shield": '<path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6l7-3z"/><path d="M9 12l2 2 4-4"/>',
    "db": '<ellipse cx="12" cy="5.5" rx="7.5" ry="2.8"/><path d="M4.5 5.5v13c0 1.6 3.4 2.8 7.5 2.8s7.5-1.2 7.5-2.8v-13"/><path d="M4.5 12c0 1.6 3.4 2.8 7.5 2.8s7.5-1.2 7.5-2.8"/>',
    "clock": '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>',
    "cloud": '<path d="M7 18h10a4 4 0 0 0 .6-7.96A6 6 0 0 0 6.1 10.5 3.75 3.75 0 0 0 7 18z"/>',
    "brain": '<path d="M9 4.5a3 3 0 0 0-3 3v.2A3 3 0 0 0 4.5 13a3 3 0 0 0 2 4.6A3 3 0 0 0 12 18V6a2.5 2.5 0 0 0-3-1.5z"/><path d="M15 4.5a3 3 0 0 1 3 3v.2A3 3 0 0 1 19.5 13a3 3 0 0 1-2 4.6A3 3 0 0 1 12 18"/>',
    "briefcase": '<rect x="3" y="7.5" width="18" height="12" rx="2"/><path d="M8.5 7.5V6a2 2 0 0 1 2-2h3a2 2 0 0 1 2 2v1.5M3 13h18"/>',
    "users": '<circle cx="9" cy="8.5" r="3.2"/><path d="M3.5 19a5.5 5.5 0 0 1 11 0"/><path d="M16 5.6a3.2 3.2 0 0 1 0 5.8M17.5 14a5.5 5.5 0 0 1 3 5"/>',
    "filter": '<path d="M4 5h16l-6 7.5V19l-4-2v-4.5L4 5z"/>',
    "chart": '<path d="M4 20V4M4 20h16"/><path d="M8 16v-4M12 16V8M16 16v-6"/>',
    "sparkle": '<path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8L12 3z"/>',
    "layers": '<path d="M12 3l9 5-9 5-9-5 9-5z"/><path d="M3 13l9 5 9-5"/>',
    "code": '<path d="M8.5 7L3.5 12l5 5M15.5 7l5 5-5 5M13.5 4.5l-3 15"/>',
    "lock": '<rect x="5" y="10.5" width="14" height="10" rx="2"/><path d="M8 10.5V7.5a4 4 0 0 1 8 0v3"/>',
    "route": '<circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="6" r="2.5"/><path d="M8.5 18H15a3 3 0 0 0 0-6H9a3 3 0 0 1 0-6h6.5"/>',
    "test": '<path d="M9 3h6M10 3v6.5L4.8 18.6A1.6 1.6 0 0 0 6.2 21h11.6a1.6 1.6 0 0 0 1.4-2.4L14 9.5V3"/><path d="M7.5 15h9"/>',
    "box": '<path d="M12 3l8 4.5v9L12 21l-8-4.5v-9L12 3z"/><path d="M4 7.5l8 4.5 8-4.5M12 12v9"/>',
}

class Diagram:
    def __init__(self, w, h, title, subtitle=""):
        self.w, self.h, self.parts = w, h, []
        self.defs = set()
        self.title, self.subtitle = title, subtitle

    def add(self, s):
        self.parts.append(s)

    def boundary(self, x, y, w, h, label, color=C["slate"], note=""):
        self.add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="16" fill="{tint(color, .035)}" stroke="{tint(color, .55)}" stroke-width="1.3" stroke-dasharray="6 5"/>')
        self.add(f'<rect x="{x + 16}" y="{y - 11}" width="{len(label) * 8.4 + 22}" height="22" rx="11" fill="{BG}" stroke="{tint(color, .55)}" stroke-width="1"/>')
        self.add(f'<text x="{x + 26}" y="{y + 4}" font-size="11.5" font-weight="600" letter-spacing=".04em" fill="{color}">{escape(label.upper())}</text>')
        if note:
            self.add(f'<text x="{x + w - 16}" y="{y + h - 12}" font-size="11" fill="{FAINT}" text-anchor="end">{escape(note)}</text>')

    def card(self, x, y, w, h, icon, title, lines=(), color=C["blue"], tag=""):
        self.add(f'<g filter="url(#shadow)"><rect x="{x}" y="{y}" width="{w}" height="{h}" rx="12" fill="{CARD}" stroke="{LINE}"/></g>')
        self.add(f'<rect x="{x}" y="{y}" width="4" height="{h}" rx="2" fill="{color}"/>')
        self.add(f'<rect x="{x + 14}" y="{y + 13}" width="32" height="32" rx="8" fill="{tint(color, .1)}"/>')
        self.add(f'<g transform="translate({x + 18} {y + 17}) scale(1)" fill="none" stroke="{color}" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">{ICONS[icon]}</g>')
        self.add(f'<text x="{x + 56}" y="{y + 27}" font-size="14" font-weight="650" fill="{INK}">{escape(title)}</text>')
        if tag:
            self.add(f'<text x="{x + 56}" y="{y + 42}" font-size="10.5" font-family="{MONO}" fill="{color}">{escape(tag)}</text>')
        ty = y + (62 if tag else 58)
        for line in lines:
            self.add(f'<text x="{x + 16}" y="{ty}" font-size="11.5" fill="{MUTED}">{escape(line)}</text>')
            ty += 16

    def chip(self, x, y, text, color=C["slate"]):
        w = len(text) * 6.3 + 16
        self.add(f'<rect x="{x}" y="{y}" width="{w}" height="20" rx="10" fill="{tint(color, .08)}" stroke="{tint(color, .3)}"/>')
        self.add(f'<text x="{x + 8}" y="{y + 14}" font-size="10.5" fill="{color}">{escape(text)}</text>')
        return w

    def arrow(self, pts, label="", color=C["slate"], dashed=False, label_at=None, both=False, anchor="middle"):
        mid = f"arrow-{color[1:]}"
        self.defs.add(f'<marker id="{mid}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z" fill="{color}"/></marker>')
        d = "M" + " L".join(f"{px} {py}" for px, py in pts)
        dash = ' stroke-dasharray="5 4"' if dashed else ""
        start = f' marker-start="url(#{mid})"' if both else ""
        self.add(f'<path d="{d}" fill="none" stroke="{color}" stroke-width="1.6"{dash} marker-end="url(#{mid})"{start}/>')
        if label:
            lx, ly = label_at or ((pts[0][0] + pts[-1][0]) / 2, (pts[0][1] + pts[-1][1]) / 2 - 8)
            for i, part in enumerate(label.split("\n")):
                w = len(part) * 6.1 + 12
                bx = lx - w / 2 if anchor == "middle" else (lx if anchor == "start" else lx - w)
                self.add(f'<rect x="{bx}" y="{ly - 12 + i * 15}" width="{w}" height="15" rx="4" fill="{BG}" opacity=".92"/>')
                tx = bx + w / 2
                self.add(f'<text x="{tx}" y="{ly + i * 15}" font-size="10.5" fill="{color}" text-anchor="middle">{escape(part)}</text>')

    def text(self, x, y, s, size=12, color=MUTED, weight=400, anchor="start"):
        self.add(f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" fill="{color}" text-anchor="{anchor}">{escape(s)}</text>')

    def svg(self):
        head = f'''<svg xmlns="http://www.w3.org/2000/svg" width="{self.w}" height="{self.h}" viewBox="0 0 {self.w} {self.h}" font-family="{FONT}">
<title>{escape(self.title)}</title>
<defs>
<filter id="shadow" x="-10%" y="-10%" width="120%" height="140%"><feDropShadow dx="0" dy="1.5" stdDeviation="2" flood-color="#1F2328" flood-opacity=".07"/></filter>
{''.join(sorted(self.defs))}
</defs>
<rect width="100%" height="100%" rx="18" fill="{BG}"/>
<text x="32" y="44" font-size="20" font-weight="700" fill="{INK}">{escape(self.title)}</text>
<text x="32" y="66" font-size="12.5" fill="{MUTED}">{escape(self.subtitle)}</text>
'''
        return head + "\n".join(self.parts) + "\n</svg>\n"
