import { test } from 'node:test';
import assert from 'node:assert/strict';
import { setup } from '../scripts/setup.ts';
import { configuration } from '../src/config.ts';

function fixture(status: string) {
  const calls: string[] = [];
  const bridge = {
    migrateBot: async () => { calls.push('migrate'); },
    installWebhook: async () => { calls.push('telegram-webhook'); },
    api: {
      ow: async (path: string, body?: any) => {
        calls.push(path || 'session');
        if (path === '') return { status };
        if (path === 'webhooks' && !body) return [];
        return { id: 'registered' };
      },
      tg: async (_token: string, method: string) => {
        calls.push(method);
        if (method === 'getMe') return { id: 42, username: 'hermes_bot' };
        if (method === 'getChat') return { is_forum: true };
        return { status: 'administrator', can_manage_topics: true };
      },
      request: async () => { calls.push('n8n'); return { accepted: true }; },
    },
  };
  return { calls, bridge };
}

test('setup accepts OpenWA ready, migrates before local Telegram calls and installs both webhooks', async () => {
  const { calls, bridge } = fixture('ready');
  const result = await setup({ ...configuration({}), admin: 'test-key' }, bridge as any);
  assert.equal(result.sessionStatus, 'ready');
  assert.ok(calls.indexOf('migrate') < calls.indexOf('getMe'));
  assert.ok(calls.indexOf('n8n') < calls.indexOf('telegram-webhook'));
  assert.equal(calls.filter(c => c === 'webhooks').length, 2);
});

test('setup refuses incomplete or disconnected sessions before migrating or registering callbacks', async () => {
  for (const status of ['qr_ready', 'disconnected', 'stopped', 'initializing']) {
    const { calls, bridge } = fixture(status);
    await assert.rejects(setup(configuration({}), bridge as any), /session is not ready/);
    assert.deepEqual(calls, ['session']);
  }
});
