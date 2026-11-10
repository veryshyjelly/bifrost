import { join } from 'node:path';
import { root } from './oracle.ts';
import { checkWorkflowBaseline, recordWorkflowBaseline } from './workflow-baseline.ts';

export const actionGoldenPath = join(root, 'test/rewrite/fixtures/action-baseline.json');
const files = ['test/reactions.test.ts', 'test/revocations.test.ts', 'test/bridge.test.ts', 'test/format.test.ts'];
const selection = {
  'test/bridge.test.ts': [
    'edits of Telegram copies explicitly cleared from the archive do not recreate them',
    'member-bot edits do not restore a sender header; master edit fallback remains attributed',
    'private member-bot messages and edit fallbacks stay nameless in the correct bot DM',
    'out-of-order WhatsApp revocation uses revokedId and prevents history resurrection',
    'child-bot group callbacks remain actionable while its ordinary group updates are ignored',
  ],
  'test/format.test.ts': [
    'phone-originated private messages and edits never link You to the conversation partner',
    'history metadata survives formatted message edits and captions do not contain raw HTML',
  ],
};
export const recordActionBaseline = () => recordWorkflowBaseline(files, selection, 'action-scenarios.ts');
if (import.meta.main) checkWorkflowBaseline(recordActionBaseline, actionGoldenPath, 'action-baseline.json');
