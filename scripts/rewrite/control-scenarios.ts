export const controlFiles = ['test/control.test.ts', 'test/group-picker.test.ts', 'test/unmirror.test.ts', 'test/bridge.test.ts', 'test/history.test.ts'];
// Unfiltered files replay every original assertion; selected files contribute
// only the control scenarios relevant to this task. Inventory IDs stay intact.
export const controlSelection: Record<string, string[]> = {
  'test/bridge.test.ts': [
    'unbound member DMs explain the missing binding instead of silently dropping the message',
    '/create in a linked whole group accepts Telegram reply-thread IDs and selects the replied-to participant',
    'configured inbox topics still require an exact topic match',
    'dedicated private bot waits for Start, then routes private replies to the contact',
    'muted conversations can resume; creating a bot at capacity leaves master routing intact',
  ],
  'test/history.test.ts': [
    'starting a bound DM retires its inbox separately and cleanup refusal does not block messaging',
    'retiring a DM topic is durable and a blocked member recreates an inbox lazily',
    'General and management topics shared with another conversation cannot be deleted by retirement',
    'a retired direct route cannot capture General or another management topic',
  ],
};
