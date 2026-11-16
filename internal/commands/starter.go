package commands

// Starter is the set `arcanflows commands init` writes (PROPOSAL-121 S1).
// Prompt-only, written for ArcanFlows; the ideas behind plan, review,
// quality-gate, research, draft-agreement and triage-inbox come from ECC's
// commands and skills (github.com/affaan-m/ECC, MIT), the wording is ours.
var Starter = map[string]string{
	"plan": `---
description: Plan a change before doing it — goals, steps, risks, how to verify
argument-hint: "[what you want to do]"
---
Plan this before anything is changed: $ARGUMENTS

1. Restate the goal in one sentence and list what is out of scope.
2. List the files, components or records that will change, and why each one.
3. Give the steps in order, each small enough to check on its own.
4. Name the risks (data loss, breaking callers, cost, security) and how each is avoided.
5. Say exactly how we will know it worked.

Do not make any change yet. End by asking me to approve the plan.
`,
	"review": `---
description: Review a change or a file for bugs, risks and missing tests
argument-hint: "[file, diff or description]"
---
Review this as a careful senior reviewer: $ARGUMENTS

Look for, in order: wrong behaviour, data loss or corruption, security issues (secrets, injection,
missing permission checks), error handling that hides failures, missing tests for the risky paths,
and only then style.

For each finding give: severity (blocker, major, minor), where (file and line or section), what goes
wrong in one concrete scenario, and the fix. If something only looks wrong, say why it is fine.
End with a one-line verdict: approve, approve with changes, or block.
`,
	"quality-gate": `---
description: Final check before calling a task done
argument-hint: "[the task]"
---
Before we call this done, check it: $ARGUMENTS

- Does it do what was asked, completely? List anything missing.
- Was it actually run or tested? Say what was run and what the output showed; do not assume.
- Are there leftovers: debug code, TODOs, temporary files, test data?
- Is the user-facing text clear, and are docs or the changelog updated if they should be?

Answer with PASS or FAIL per point and one line of evidence each.
`,
	"weekly-report": `---
description: Weekly report — what changed, the numbers, what needs attention
argument-hint: "[week or period, topic]"
---
Write the weekly report for: $ARGUMENTS

Use the data you can reach (data tables, tools, the knowledge base). Structure:
1. Headline: the one thing that matters most this week, with its number.
2. Key figures against the previous week, as a short table.
3. What went well and what slipped, two or three bullets each.
4. Three actions for next week, each with an owner if known.

Name the source of every figure. If a figure is not available, say so instead of estimating.
`,
	"triage-inbox": `---
description: Triage messages into act now / today / this week / no action, with drafts
argument-hint: "[mailbox, channel or pasted messages]"
---
Triage these messages: $ARGUMENTS

Put each one in exactly one group: Act now (blocking someone or time-critical), Today, This week,
No action (FYI, newsletters, already handled). For each message give: sender, subject or gist, the
group, and one line on why.

For Act now and Today, draft a short reply. Drafts are proposals: do not send anything. Never
include card numbers, passwords or personal data you did not need in a reply.
`,
	"research": `---
description: Research a question with sources — findings, evidence, open questions
argument-hint: "[question]"
---
Research this: $ARGUMENTS

Search more than one source and open the pages you rely on; a search snippet is not a source.
Give: a two-sentence answer first, then the findings with a citation (title and link) after each
claim, then where sources disagree, then what is still unknown. Mark anything you could not verify
as unverified. Prefer primary sources (official docs, filings, the original study).
`,
	"draft-agreement": `---
description: Draft an agreement from a template and the deal terms — always a DRAFT
argument-hint: "[counterparty and terms, or the template to use]"
---
Draft an agreement for: $ARGUMENTS

Start from the matching document template if one exists. Fill only terms that were given; leave
every missing term as a clearly marked [TO CONFIRM: …] placeholder rather than inventing it.
Title the document DRAFT, list the placeholders at the end, and note anything unusual in the terms.
A finished draft is not approval to sign: say who must review it before it is sent.
`,
	"handoff-summary": `---
description: Summarise a conversation for a human taking over
argument-hint: "[conversation or ticket]"
---
Prepare a handoff for a person taking over: $ARGUMENTS

In under 150 words: who the customer is, what they want, what has been tried and what happened,
what was promised and by when, the customer's mood, and the single next step. List any data the
person must not repeat back (payment details, passwords).
`,
}
