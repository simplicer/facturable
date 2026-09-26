# ADR-001: Monorepo Go + SvelteKit + Docker

Decision: single repo. Go for agent/api/cli (user mandate: "todo lo que pueda ser en Go, en Go"), SvelteKit only for web, docker-compose for every orchestrated service, Makefile as single entrypoint (ralph-loop).
Consequences: shared pure core (internal/verifactu) reused by agent and api; web isolated in web/.
