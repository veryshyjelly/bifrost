import { join } from 'node:path';
import { root } from './oracle.ts';
import { checkWorkflowBaseline, recordWorkflowBaseline } from './workflow-baseline.ts';

export const historyGoldenPath = join(root, 'test/rewrite/fixtures/history-baseline.json');
export const recordHistoryBaseline = () => recordWorkflowBaseline(['test/history.test.ts'], {}, 'history-scenarios.ts', { omitIdleTicks: true });
if (import.meta.main) checkWorkflowBaseline(recordHistoryBaseline, historyGoldenPath, 'history-baseline.json');
