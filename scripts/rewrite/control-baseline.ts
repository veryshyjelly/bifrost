import { join } from 'node:path';
import { root } from './oracle.ts';
import { controlFiles, controlSelection } from './control-scenarios.ts';
import { checkWorkflowBaseline, recordWorkflowBaseline } from './workflow-baseline.ts';

export const controlGoldenPath = join(root, 'test/rewrite/fixtures/control-baseline.json');
export const recordControlBaseline = () => recordWorkflowBaseline(controlFiles, controlSelection);
if (import.meta.main) checkWorkflowBaseline(recordControlBaseline, controlGoldenPath, 'control-baseline.json');
