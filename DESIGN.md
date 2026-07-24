---
version: alpha
name: Miniform
description: A quiet, private inbox for every form on your websites.
colors:
  primary: "#4E6653"
  primary-deep: "#34473A"
  primary-pale: "#DFE8E1"
  ink: "#242820"
  ink-muted: "#686F65"
  surface: "#F5F1E8"
  surface-raised: "#FFFEFA"
  surface-secondary: "#E8E0D1"
  border: "#D4CBBB"
  info: "#526A78"
  warning: "#A46A2A"
  error: "#9B4335"
typography:
  display:
    fontFamily: "Iowan Old Style, Palatino Linotype, Palatino, Georgia, serif"
    fontSize: "48px"
    fontWeight: 700
    lineHeight: 1
    letterSpacing: "-0.03em"
  headline:
    fontFamily: "Iowan Old Style, Palatino Linotype, Palatino, Georgia, serif"
    fontSize: "30px"
    fontWeight: 700
    lineHeight: 1.1
    letterSpacing: "-0.025em"
  title:
    fontFamily: "Iowan Old Style, Palatino Linotype, Palatino, Georgia, serif"
    fontSize: "24px"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "-0.02em"
  body:
    fontFamily: "Avenir Next, Avenir, Segoe UI, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 400
    lineHeight: 1.5
  control:
    fontFamily: "Avenir Next, Avenir, Segoe UI, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.43
  button:
    fontFamily: "Avenir Next, Avenir, Segoe UI, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 600
    lineHeight: 1.43
  label:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace"
    fontSize: "11px"
    fontWeight: 700
    lineHeight: 1
    letterSpacing: "0.2em"
rounded:
  sm: "2px"
  md: "8px"
  lg: "10px"
  full: "9999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "32px"
  2xl: "48px"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.surface-raised}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: "10px 16px"
  button-primary-hover:
    backgroundColor: "{colors.primary-deep}"
    textColor: "{colors.surface-raised}"
  button-secondary:
    backgroundColor: "{colors.surface-raised}"
    textColor: "{colors.ink}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: "10px 16px"
  input:
    backgroundColor: "{colors.surface-raised}"
    textColor: "{colors.ink}"
    typography: "{typography.control}"
    rounded: "{rounded.md}"
    padding: "10px 12px"
    minHeight: "44px"
  section:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    padding: "0px"
---

# Design System: Miniform

## Overview

**Creative North Star: "The Field Notebook"**

Miniform feels like a well-kept field notebook for an operator: quiet paper, dark ink, precise rules, and one restrained moss accent. The interface is calm but information-dense, with familiar controls and explicit operational state. Visual character supports the workflow; it never competes with entries, endpoints, delivery, or safeguards.

The system rejects generic SaaS dashboards, ornamental data visualization, decorative motion, excessive cards, fashionable effects, and promotional admin surfaces. Responsive behavior is structural: navigation and tables adapt, while typography stays stable enough to preserve an operator's scanning rhythm.

**Key Characteristics:**

- Quiet, task-first surfaces with one restrained accent.
- Explicit state, readable density, and familiar controls.
- Open composition, with dividers and spacing before containment.
- Flat surfaces with limited, purposeful shape.
- Motion reserved for state and feedback.

## Colors

The palette combines a paper-and-ink neutral foundation with a single moss interaction color and distinct semantic colors used only for status.

### Primary

- **Moss** (`primary`): primary actions, links, focus, current selection, and accepted state.
- **Deep Moss** (`primary-deep`): hover and active treatment.
- **Pale Moss** (`primary-pale`): quiet success and selected backgrounds.

### Secondary

- **Slate Signal** (`info`): informational and pending delivery state.
- **Ochre Warning** (`warning`): warnings that require attention without implying failure.
- **Clay Error** (`error`): validation, failed delivery, and destructive state.

### Neutral

- **Ink** (`ink`): main text and dark navigation surfaces.
- **Muted Ink** (`ink-muted`): supporting text that still meets WCAG 2.2 AA contrast.
- **Paper** (`surface`): page background.
- **Raised Paper** (`surface-raised`): controls and content surfaces.
- **Parchment** (`surface-secondary`): secondary surfaces and quiet grouping.
- **Rule** (`border`): borders and dividers.

### Named Rules

**The One Accent Rule.** Moss carries interaction; semantic colors appear only when they communicate state.

**The Paper Contrast Rule.** Supporting text must remain readable on paper and raised paper; light gray is forbidden as body copy.

## Typography

**Display Font:** Iowan Old Style (with Palatino and Georgia fallbacks)

**Body Font:** Avenir Next (with Avenir, Segoe UI, and system fallbacks)
**Label/Mono Font:** system monospace

**Character:** The serif display face gives page titles permanence without turning controls into editorial decoration. The sans body stays practical and quiet; monospace is reserved for tokens, timestamps, paths, and compact operational labels.

### Hierarchy

- **Display** (700, 48px, 1): page titles on wide screens; reduce to 36px on compact screens.
- **Headline** (700, 30px, 1.1): section introductions and strong empty-state guidance.
- **Title** (700, 24px, 1.2): named endpoints, delivery routes, and local groups.
- **Body** (400, 16px, 1.5): instructions and descriptions, capped near 70ch for prose.
- **Control** (400, 14px, 1.43): inputs and compact interactive values.
- **Button** (600, 14px, 1.43): primary and secondary actions.
- **Label** (700, 11px, 0.2em): short operational metadata only; uppercase kickers are scarce.

### Named Rules

**The Task-Type Rule.** Display type is forbidden in buttons, form labels, navigation labels, and data cells.

**The Tightness Floor.** Display letter spacing never goes below -0.04em.

## Elevation

The system is flat by default. Depth comes from spacing, dark navigation contrast, local surface changes, and dividers; decorative shadows are absent. Focus rings may create a temporary halo because they communicate keyboard state rather than elevation.

### Named Rules

**The Flat-by-Default Rule.** A resting surface has no drop shadow. Use a border or tonal change to establish hierarchy.

**The State-Only Motion Rule.** Transitions run for 150–200ms and communicate hover, focus, loading, or replacement; page-load choreography is forbidden.

## Components

### Buttons

- **Shape:** restrained 8px corners (`rounded.md`).
- **Primary:** moss surface, raised-paper text, 10px by 16px padding; use once per decision area.
- **Hover / Focus:** deep moss on hover and a visible moss focus ring; no lift, glow, or decorative shadow.
- **Secondary:** raised paper, ink text, and a rule border that changes to moss on hover.

### Cards / Containers

- **Default:** no container. Related content shares alignment and spacing before it receives a surface.
- **Corner Style:** restrained corners (`rounded.lg`) only when a functional control group needs containment.
- **Background:** paper by default; raised paper is reserved for inputs and genuinely interactive regions.
- **Shadow Strategy:** none.
- **Border:** use a divider for sections and tables; colored side stripes and decorative perimeter borders are forbidden.
- **Internal Padding:** belongs to functional groups, not generic page sections.

### Inputs / Fields

- **Style:** raised paper, ink text, rule border, 8px corners, at least 44px high, 12px horizontal padding, and readable placeholder text.
- **Focus:** moss border plus a narrow translucent moss ring.
- **Error / Disabled:** clay is reserved for error; disabled controls retain readable labels and clear loss of affordance.

### Endpoint source

Starter HTML is supporting material, not the primary task. It always appears as the final section on endpoint create, edit, and detail pages, separated from the workflow by one rule.

### Navigation

The desktop shell uses a dark ink sidebar with open navigation labels. The current route uses brighter text and stronger weight without a pill or card background. Mobile navigation becomes a horizontally scrollable row. Active state is communicated by contrast, weight, and `aria-current`, never color alone.

### Status

Compact status badges may use a full pill because they are tags. Accepted, pending, warning, and failed states combine text with semantic color; operational meaning never depends on the dot or hue alone.

## Do's and Don'ts

### Do:

- **Do** use moss for the primary action, focus, current selection, and accepted state.
- **Do** use dividers and whitespace before adding a container.
- **Do** leave page headers, lists, and forms open unless containment explains a real relationship.
- **Do** use one separator at a section boundary; never stack a parent border with a child divider.
- **Do** show what happened, when it happened, and the next available action.
- **Do** maintain WCAG 2.2 AA contrast, visible focus, reduced-motion support, and keyboard-complete workflows.
- **Do** keep ordinary control and container radii between 2px and 10px.

### Don't:

- **Don't** create generic SaaS dashboards, promotional admin surfaces, or ornamental data visualizations.
- **Don't** use decorative motion, gradients, filler illustrations, hype, glassmorphism, or decorative shadows.
- **Don't** build excessive or nested card grids when a divider or shared surface is sufficient.
- **Don't** wrap ordinary sections in raised-paper boxes.
- **Don't** use colored side stripes on cards, callouts, list items, or alerts.
- **Don't** use display fonts in controls or depend on color alone for operational state.
