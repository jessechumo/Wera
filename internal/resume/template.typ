// Wera resume template: the classic one-column layout of the widely used
// LaTeX "Jake's resume" (small-caps name and section titles under a rule,
// two-column entry rows, compact bullets), typeset in New Computer Modern.
//
// All content comes from resume.json and layout.json as data, never as
// markup, so nothing a user types can run as Typst code.
#let data = json("resume.json")
#let lay = json("layout.json")
#let fs = lay.font_size * 1pt
#let sp = lay.spacing
#let small = fs * 10 / 11
#let large = fs * 12 / 11
#let huge = fs * 24.88 / 11

#set document(title: data.name + " - Resume", author: data.name)
#set page(paper: "us-letter", margin: (x: lay.margin * 1in, top: lay.margin * 0.85in, bottom: lay.margin * 1in))
#set text(font: "New Computer Modern", size: fs, lang: "en", hyphenate: false)
#set par(leading: 0.52em * sp, spacing: 0.6em * sp, justify: false)
#set list(indent: 0.12in, body-indent: 0.5em, spacing: 0.32em * sp, marker: text(size: small * 0.9)[•])

#let maybe-link(url, body) = if url != none and url != "" { link(url, body) } else { body }
#let field(e, k) = e.at(k, default: "")
#let visible(items) = items.filter(x => not x.at("hidden", default: false))

// Header: name, then contact details separated by bars.
#align(center)[
  #text(size: huge, smallcaps(data.name))
  #v(-0.35em)
  #{
    let parts = ()
    if data.at("phone", default: "") != "" { parts.push(data.phone) }
    if data.at("email", default: "") != "" { parts.push(link("mailto:" + data.email, data.email)) }
    if data.at("location", default: "") != "" { parts.push(data.location) }
    for l in data.at("links", default: ()) { parts.push(link(l.url, l.label)) }
    text(size: small, parts.join[ #h(0.25em)|#h(0.25em) ])
  }
]

#let section-title(title) = block(above: 0.85em * sp, below: 0.45em * sp, width: 100%)[
  #text(size: large, smallcaps(title))
  #v(-0.65em)
  #line(length: 100%, stroke: 0.45pt)
]

#let row(lhs, rhs) = grid(columns: (1fr, auto), column-gutter: 1em, lhs, align(right, rhs))

#let entries(s) = {
  for e in visible(s.at("entries", default: ())) {
    block(above: 0.55em * sp, below: 0em, pad(left: 0.15in, {
      row(strong(maybe-link(field(e, "url"), field(e, "heading"))), field(e, "dates"))
      if field(e, "subheading") != "" or field(e, "location") != "" {
        v(-0.3em)
        row(text(size: small, emph(field(e, "subheading"))), text(size: small, emph(field(e, "location"))))
      }
      let bullets = visible(e.at("bullets", default: ()))
      if bullets.len() > 0 {
        v(0.15em * sp)
        set text(size: small)
        list(..bullets.map(b => b.text))
      }
    }))
  }
}

#let projects(s) = {
  for e in visible(s.at("entries", default: ())) {
    block(above: 0.55em * sp, below: 0em, pad(left: 0.15in, {
      row(strong(field(e, "heading")), text(size: small, emph(field(e, "tech"))))
      if field(e, "text") != "" {
        v(-0.25em)
        text(size: small, field(e, "text"))
        if field(e, "url") != "" {
          let label = field(e, "link_label")
          text(size: small)[ #link(e.url, underline(if label != "" { label } else { "link" }))]
        }
      }
      let bullets = visible(e.at("bullets", default: ()))
      if bullets.len() > 0 {
        set text(size: small)
        list(..bullets.map(b => b.text))
      }
    }))
  }
}

#let compact(s) = {
  for e in visible(s.at("entries", default: ())) {
    block(above: 0.45em * sp, below: 0em, pad(left: 0.15in,
      row([#strong(field(e, "heading")) #h(0.2em)|#h(0.2em) #text(size: small, emph(field(e, "subheading")))],
          text(size: small, emph(field(e, "dates"))))))
  }
}

#let skills(s) = pad(left: 0.15in, text(size: small, {
  for g in s.at("skills", default: ()) {
    [#strong(g.name + ":") #g.items]
    linebreak()
  }
}))

#for s in visible(data.sections) {
  section-title(s.title)
  if s.kind == "entries" { entries(s) }
  else if s.kind == "projects" { projects(s) }
  else if s.kind == "compact" { compact(s) }
  else if s.kind == "skills" { skills(s) }
  else if s.kind == "summary" { pad(left: 0.15in, text(size: small, s.at("text", default: ""))) }
}

// Where the content ends, for one-page fitting.
#context [#metadata((page: here().page(), y: here().position().y.pt())) <wera-end>]
