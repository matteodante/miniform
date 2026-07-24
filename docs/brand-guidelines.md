# Miniform brand guidelines

> Status: active
> Product idea: a quiet, private inbox for every form on your websites.

## Quick reference

| Element | Value |
|---|---|
| Primary Color | #4E6653 |
| Secondary Color | #242820 |
| Accent Color | #4E6653 |
| Display Font | Iowan Old Style |
| Body Font | Avenir Next |
| Voice | Quiet, direct, operational |

## Positioning

Miniform is the self-hosted form inbox for small teams that want reliable collection, delivery and spam protection without sending customer data through a form SaaS.

Primary message: **Every form response, in your own quiet inbox.**

Proof points:

- One binary and one SQLite database.
- Email and webhook delivery with retries.
- Origin controls, rate limiting and captcha support.
- Files and submission data stay on the operator's server.

## Product language

| Prefer | Avoid in the interface |
|---|---|
| Inbox | Dashboard |
| Entries | Submissions |
| Endpoints | Forms |
| Delivery | Integrations |
| Safeguards | Turnstile credentials |
| Workspace | General settings |

Code and public HTTP paths use precise domain terms when product language would be ambiguous.

## Voice

### Brand personality

| Trait | We are | We are not |
|---|---|---|
| Quiet | Calm and focused | Empty or vague |
| Direct | Specific and brief | Abrupt or cryptic |
| Practical | Grounded in the task | Feature-led or promotional |
| Protective | Clear about data ownership | Alarmist |

Use sentence case. State what happened and what the user can do next. Success messages end without exclamation marks.

Avoid: seamless, powerful, next-generation, elevate, unlock, revolutionary, best-in-class and SaaS marketing clichés.

## Visual identity

The interface should feel like a well-kept field notebook: warm paper, dark ink, precise rules and a single moss accent. It is editorial rather than dashboard-like.

### Primary colors

| Name | Hex | Usage |
|---|---|---|
| Moss | #4E6653 | Links, focus, primary actions |
| Deep Moss | #34473A | Hover and active states |

### Secondary colors

| Name | Hex | Usage |
|---|---|---|
| Ink | #242820 | Main text and dark surfaces |
| Paper | #F5F1E8 | Page background |

### Neutral colors

| Name | Hex | Usage |
|---|---|---|
| Parchment | #E8E0D1 | Secondary surfaces |
| Rule | #D4CBBB | Borders and dividers |
| Muted Ink | #686F65 | Supporting text |
| White | #FFFEFA | Raised surfaces |

### Semantic colors

| State | Hex |
|---|---|
| Success | #4E6653 |
| Warning | #A46A2A |
| Error | #9B4335 |
| Info | #526A78 |

### Typography

```css
--font-display: "Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif;
--font-body: "Avenir Next", Avenir, "Segoe UI", sans-serif;
--font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
```

Display type is reserved for product name and page headings. Counts, tokens and timestamps use tabular numerals or monospace.

## Interface rules

- Inbox is the first navigation item and the post-login destination.
- Prefer dividers and whitespace to generic card grids.
- Use one accent color; semantic colors appear only for status.
- Corners stay restrained: 2–10px, never pills for ordinary controls.
- Every page has a visible title, active navigation state and keyboard focus.
- Empty states explain the next concrete action.
- No gradients, decorative shadows, debug logging or filler illustrations.
