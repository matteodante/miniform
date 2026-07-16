package forms

import (
	"strings"
)

// FormTemplate represents a pre-configured form template.
type FormTemplate struct {
	ID              string
	Name            string
	Description     string
	Slug            string
	Icon            string
	Color           string
	ComingSoon      bool
	WIP             bool
	HTML            string
	WebhookDelivery WebhookDelivery
	EmailDelivery   EmailDelivery
}

// GetFormTemplates returns all available form templates.
func GetFormTemplates() []FormTemplate {
	return []FormTemplate{
		{
			ID:          "contact",
			Name:        "Contact Form",
			Description: "Simple contact form with name, email, and message fields",
			Slug:        "contact",
			Icon:        "💬",
			Color:       "blue",
			HTML:        contactTemplateHTML,
			EmailDelivery: EmailDelivery{
				Enabled: true,
			},
		},
		{
			ID:          "feedback",
			Name:        "Feedback Form",
			Description: "Collect user feedback and feature requests",
			Slug:        "feedback",
			Icon:        "💡",
			Color:       "purple",
			HTML:        feedbackTemplateHTML,
			EmailDelivery: EmailDelivery{
				Enabled: true,
			},
		},
		{
			ID:          "bug-report",
			Name:        "Bug Report",
			Description: "Help users report bugs and technical issues",
			Slug:        "bug-report",
			Icon:        "🐛",
			Color:       "red",
			HTML:        bugTemplateHTML,
			EmailDelivery: EmailDelivery{
				Enabled: true,
			},
		},
		{
			ID:          "newsletter",
			Name:        "Newsletter Signup",
			Description: "Collect email addresses for your newsletter",
			Slug:        "newsletter",
			Icon:        "📧",
			Color:       "green",
			HTML:        newsletterTemplateHTML,
			EmailDelivery: EmailDelivery{
				Enabled: true,
			},
		},
		{
			ID:          "waitlist",
			Name:        "Waitlist",
			Description: "Build a waitlist for your product launch",
			Slug:        "waitlist",
			Icon:        "⏳",
			Color:       "yellow",
			HTML:        waitlistTemplateHTML,
			EmailDelivery: EmailDelivery{
				Enabled: true,
			},
		},
		{
			ID:              "blank",
			Name:            "Blank Form",
			Description:     "Start from scratch with an empty form",
			Slug:            "",
			Icon:            "📝",
			Color:           "gray",
			HTML:            blankTemplateHTML,
			EmailDelivery:   EmailDelivery{},
			WebhookDelivery: WebhookDelivery{},
		},
	}
}

// GetTemplateByID returns a specific template by ID.
func GetTemplateByID(id string) *FormTemplate {
	templates := GetFormTemplates()
	for _, t := range templates {
		if t.ID == id {
			template := t // copy to avoid referencing loop variable
			return &template
		}
	}
	return nil
}

// RenderHTML returns the template HTML with the form action placeholder replaced.
func (t *FormTemplate) RenderHTML(action string) string {
	if t == nil || strings.TrimSpace(t.HTML) == "" {
		return ""
	}

	if strings.TrimSpace(action) == "" {
		action = "/forms/your-form/submit?token=YOUR_FORM_TOKEN"
	}

	return strings.ReplaceAll(t.HTML, "{{FORM_ACTION}}", action)
}

const sharedTemplateStyles = `
<style>
	.miniform-shell {
		max-width: 520px;
		margin: 24px auto;
		background: #fffefa;
		border-radius: 4px;
		padding: 32px;
		border: 1px solid #d4cbbb;
		box-shadow: 0 18px 50px rgba(36, 40, 32, 0.08);
		font-family: 'Avenir Next', Avenir, 'Segoe UI', sans-serif;
		color: #242820;
	}

	.miniform-eyebrow {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 0 0 4px;
		border-bottom: 1px solid #4e6653;
		font-size: 12px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.16em;
		color: #4e6653;
	}

	.miniform-shell h2 {
		font-size: 1.6rem;
		margin: 0.85rem 0 0.4rem;
		font-family: 'Iowan Old Style', 'Palatino Linotype', Palatino, Georgia, serif;
		letter-spacing: -0.025em;
	}

	.miniform-shell p {
		margin: 0;
		color: #6e756b;
		font-size: 0.95rem;
	}

	.miniform-stack {
		display: flex;
		flex-direction: column;
		gap: 18px;
		margin-top: 1.5rem;
	}

	.miniform-row {
		display: flex;
		flex-wrap: wrap;
		gap: 16px;
	}

	.miniform-field {
		flex: 1;
		min-width: 160px;
	}

	.miniform-field span {
		display: block;
		font-size: 0.85rem;
		font-weight: 600;
		color: #3d443c;
		margin-bottom: 6px;
	}

	.miniform-field input,
	.miniform-field select,
	.miniform-field textarea {
		width: 100%;
		border: 1px solid #d4cbbb;
		border-radius: 3px;
		padding: 12px 14px;
		font-size: 0.95rem;
		transition: border 0.2s ease, box-shadow 0.2s ease;
		background: #f5f1e8;
	}

	.miniform-field textarea {
		min-height: 120px;
		resize: vertical;
	}

	.miniform-field input:focus,
	.miniform-field textarea:focus,
	.miniform-field select:focus {
		outline: none;
		border-color: #4e6653;
		box-shadow: 0 0 0 3px rgba(78, 102, 83, 0.15);
		background: #fffefa;
	}

	.miniform-helper {
		font-size: 0.8rem;
		color: #6e756b;
		margin-top: 4px;
	}

	.miniform-button {
		width: 100%;
		border: none;
		border-radius: 4px;
		padding: 14px 18px;
		font-size: 1rem;
		font-weight: 600;
		color: #fffefa;
		background: #4e6653;
		cursor: pointer;
		transition: transform 0.2s ease, background 0.2s ease;
	}

	.miniform-button:hover {
		transform: translateY(-1px);
		background: #34473a;
	}

	.miniform-checkbox {
		display: flex;
		align-items: flex-start;
		gap: 12px;
		font-size: 0.9rem;
		color: #3d443c;
	}

	.miniform-checkbox input {
		width: 18px;
		height: 18px;
		margin-top: 3px;
	}
</style>
`

const contactTemplateHTML = `
<div class="miniform-shell">
	<div class="miniform-eyebrow">Contact</div>
	<h2>Contact our team</h2>
	<p>Tell us how we can help and someone will reply within one business day.</p>

	<form action="{{FORM_ACTION}}" method="POST" class="miniform-stack">
		<div class="miniform-row">
			<label class="miniform-field">
				<span>Full name</span>
				<input type="text" name="name" placeholder="Alex Rivers" required>
			</label>
			<label class="miniform-field">
				<span>Company</span>
				<input type="text" name="company" placeholder="Acme Inc.">
			</label>
		</div>

		<div class="miniform-row">
			<label class="miniform-field">
				<span>Email address</span>
				<input type="email" name="email" placeholder="you@example.com" required>
			</label>
			<label class="miniform-field">
				<span>Topic</span>
				<select name="topic" required>
					<option value="">Choose a topic</option>
					<option>Product question</option>
					<option>Billing</option>
					<option>Partnership</option>
					<option>Something else</option>
				</select>
			</label>
		</div>

		<label class="miniform-field">
			<span>How can we help?</span>
			<textarea name="message" placeholder="Share details about your request" required></textarea>
		</label>

		<label class="miniform-checkbox">
			<input type="checkbox" name="consent" required>
			<span>I agree to be contacted about this request.</span>
		</label>

		<button type="submit" class="miniform-button">Send message</button>
	</form>
</div>
` + sharedTemplateStyles

const newsletterTemplateHTML = `
<div class="miniform-shell">
	<div class="miniform-eyebrow">Newsletter</div>
	<h2>Join the newsletter</h2>
	<p>Receive product updates, launch notes, and best practices twice a month.</p>

	<form action="{{FORM_ACTION}}" method="POST" class="miniform-stack">
		<label class="miniform-field">
			<span>Email address</span>
			<input type="email" name="email" placeholder="you@company.com" required>
		</label>

		<label class="miniform-field">
			<span>First name</span>
			<input type="text" name="first_name" placeholder="Jamie" required>
		</label>

		<label class="miniform-field">
			<span>What would you like to hear about?</span>
			<select name="interest">
				<option>Product updates</option>
				<option>Growth stories</option>
				<option>Weekly tips</option>
			</select>
		</label>

		<label class="miniform-checkbox">
			<input type="checkbox" name="consent" required>
			<span>I agree to receive occasional product emails.</span>
		</label>

		<button type="submit" class="miniform-button">Subscribe</button>
	</form>
</div>
` + sharedTemplateStyles

const waitlistTemplateHTML = `
<div class="miniform-shell">
	<div class="miniform-eyebrow">Waitlist</div>
	<h2>Join the early access list</h2>
	<p>We’re releasing limited invites. Tell us a bit about your team and we’ll keep you posted.</p>

	<form action="{{FORM_ACTION}}" method="POST" class="miniform-stack">
		<div class="miniform-row">
			<label class="miniform-field">
				<span>Full name</span>
				<input type="text" name="name" placeholder="Morgan Lee" required>
			</label>
			<label class="miniform-field">
				<span>Company</span>
				<input type="text" name="company" placeholder="Northwind">
			</label>
		</div>

		<div class="miniform-row">
			<label class="miniform-field">
				<span>Work email</span>
				<input type="email" name="email" placeholder="you@company.com" required>
			</label>
			<label class="miniform-field">
				<span>Team size</span>
				<select name="team_size">
					<option value="">Select</option>
					<option>1-5 people</option>
					<option>6-25 people</option>
					<option>26-100 people</option>
					<option>100+ people</option>
				</select>
			</label>
		</div>

		<label class="miniform-field">
			<span>What will you use us for?</span>
			<textarea name="use_case" placeholder="Share how your team would use the product" required></textarea>
		</label>

		<button type="submit" class="miniform-button">Request invite</button>
	</form>
</div>
` + sharedTemplateStyles

const feedbackTemplateHTML = `
<div class="miniform-shell">
	<div class="miniform-eyebrow">Feedback</div>
	<h2>Share your feedback</h2>
	<p>Help us build the roadmap. Tell us what’s working and what could be better.</p>

	<form action="{{FORM_ACTION}}" method="POST" class="miniform-stack">
		<div class="miniform-row">
			<label class="miniform-field">
				<span>Name</span>
				<input type="text" name="name" placeholder="Taylor" required>
			</label>
			<label class="miniform-field">
				<span>Email</span>
				<input type="email" name="email" placeholder="you@example.com">
			</label>
		</div>

		<label class="miniform-field">
			<span>How satisfied are you?</span>
			<select name="satisfaction" required>
				<option value="">Choose a score</option>
				<option>Very satisfied</option>
				<option>Satisfied</option>
				<option>Neutral</option>
				<option>Unsatisfied</option>
			</select>
		</label>

		<label class="miniform-field">
			<span>Feature or area</span>
			<input type="text" name="feature" placeholder="Exports, notifications, ...">
		</label>

		<label class="miniform-field">
			<span>Comments</span>
			<textarea name="comments" placeholder="What should we improve?" required></textarea>
		</label>

		<button type="submit" class="miniform-button">Send feedback</button>
	</form>
</div>
` + sharedTemplateStyles

const bugTemplateHTML = `
<div class="miniform-shell">
	<div class="miniform-eyebrow">Bug report</div>
	<h2>Report an issue</h2>
	<p>Found something off? Share the details and we’ll investigate within a few hours.</p>

	<form action="{{FORM_ACTION}}" method="POST" class="miniform-stack">
		<div class="miniform-row">
			<label class="miniform-field">
				<span>Name</span>
				<input type="text" name="reporter" placeholder="Jordan" required>
			</label>
			<label class="miniform-field">
				<span>Email</span>
				<input type="email" name="email" placeholder="you@example.com" required>
			</label>
		</div>

		<div class="miniform-row">
			<label class="miniform-field">
				<span>Severity</span>
				<select name="severity" required>
					<option value="">Select severity</option>
					<option>Low</option>
					<option>Medium</option>
					<option>High</option>
					<option>Critical</option>
				</select>
			</label>
			<label class="miniform-field">
				<span>Area of the product</span>
				<input type="text" name="area" placeholder="Account settings">
			</label>
		</div>

		<label class="miniform-field">
			<span>Steps to reproduce</span>
			<textarea name="steps" placeholder="1. Go to..., 2. Click..." required></textarea>
			<div class="miniform-helper">Include as much detail as possible.</div>
		</label>

		<label class="miniform-field">
			<span>Expected vs. actual behavior</span>
			<textarea name="expected" placeholder="Expected X but saw Y"></textarea>
		</label>

		<button type="submit" class="miniform-button">Submit bug</button>
	</form>
</div>
` + sharedTemplateStyles

const blankTemplateHTML = `
<div class="miniform-shell">
	<div class="miniform-eyebrow">Simple form</div>
	<h2>Let’s collect data</h2>
	<p>Use this lightweight template as a starting point.</p>

	<form action="{{FORM_ACTION}}" method="POST" class="miniform-stack">
		<label class="miniform-field">
			<span>Field label</span>
			<input type="text" name="field_one" placeholder="Text input">
		</label>

		<label class="miniform-field">
			<span>Message</span>
			<textarea name="field_two" placeholder="Textarea input"></textarea>
		</label>

		<button type="submit" class="miniform-button">Submit</button>
	</form>
</div>
` + sharedTemplateStyles
