/**
 * Analytics module barrel export.
 *
 * Re-exports the database layer, session engine, authorship ledger,
 * and stats query interface.
 */

export { AnalyticsDB } from "./db.js";
export type { PreparedStatements } from "./db.js";
export { AnalyticsEngine } from "./session.js";
export { AuthorshipLedger } from "./authorship.js";
export {
  queryStats,
  queryDailySummary,
  queryToolBreakdown,
  queryAuthorshipSummary,
} from "./stats.js";
