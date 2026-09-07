import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Next.js 16's `next dev` otherwise (re)generates frontend/AGENTS.md and
  // frontend/CLAUDE.md on every dev server start — this repo already has
  // one canonical CLAUDE.md at the project root; a second, conflicting
  // one inside frontend/ would be confusing, not helpful.
  agentRules: false,
};

export default nextConfig;
