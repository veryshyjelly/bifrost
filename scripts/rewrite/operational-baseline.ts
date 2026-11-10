import { join } from 'node:path';
import { root } from './oracle.ts';
import { checkWorkflowBaseline, recordWorkflowBaseline } from './workflow-baseline.ts';

export const operationalGoldenPath = join(root, 'test/rewrite/fixtures/operational-baseline.json');
const selection = {
  'test/bridge.test.ts': ['standalone management and legacy forwarding jobs converge on one durable action without HTTP forwarding'],
  'test/transfer.test.ts': ['import keeps credentials literal, normalizes only local paths and removes the retired n8n setting', 'export discovers legacy and Bifrost named volumes and refuses ambiguous or bind-mounted state'],
};
export const recordOperationalBaseline = () => recordWorkflowBaseline(['test/configure.test.ts', 'test/setup.test.ts', 'test/transfer.test.ts', 'test/identity.test.ts', 'test/bridge.test.ts'], selection, 'operational-scenarios.ts',
  { omitIdleTicks: true, freezeDateConstructor: true, observeTooling: true });
if (import.meta.main) checkWorkflowBaseline(recordOperationalBaseline, operationalGoldenPath, 'operational-baseline.json');
