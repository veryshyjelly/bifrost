import { join } from 'node:path';
import { root } from './oracle.ts';
import { checkWorkflowBaseline, recordWorkflowBaseline } from './workflow-baseline.ts';

export const statusMetadataGoldenPath = join(root, 'test/rewrite/fixtures/status-metadata-baseline.json');
export const recordStatusMetadataBaseline = () => recordWorkflowBaseline(['test/statuses.test.ts', 'test/sync.test.ts'], {}, 'metadata-scenarios.ts',
  { omitIdleTicks: true, captureUploadBytes: true, aliasPhotoPaths: true, freezeDateConstructor: true });
if (import.meta.main) checkWorkflowBaseline(recordStatusMetadataBaseline, statusMetadataGoldenPath, 'status-metadata-baseline.json');
