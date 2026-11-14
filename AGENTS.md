# Project instructions

## Planning

Keep plans, spike plans, roadmaps, and task breakdowns outside this repository. Do not create or maintain planning documents in the repo; discuss proposed work in the conversation or the project's external planning tools.

## Git workflow

Make changes on a separate branch and submit them through a pull request. Never push directly to the repository's default branch.

Create each independent branch from the latest remote default branch. If work depends on an unmerged pull request, stack it on that pull request with GitHub stacks (`gh stack`) instead of waiting for the merge. Never base plain branches on each other outside a stack.
