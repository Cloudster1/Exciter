# Introduction

Rotor runs workflows for the suite. A workflow strings together model agents, tool calls, Python and approvals; people
start it by hand, on a schedule or from an event of another tool, and every run acts in Exciter, Armature and Stator
as the person who started it. Rotor has not been released yet; this specification says what it shall do once it is,
and the requirements marked as approved only are the ones not built yet.

It covers writing and sharing workflows, running them, approvals, the connections to the other tools, triggers, the
assistants that work with Rotor, and the administration of the installation. Terms follow Rotor's own: a workflow is
saved as numbered versions, a run pins one version, a binding names a tool and the calls a node may make through it,
and a connection is a person's consent in a tool.
