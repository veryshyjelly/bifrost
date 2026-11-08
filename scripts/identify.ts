import { configuration } from '../src/config.ts';
const c = configuration();
if (!c.masterToken) throw new Error('Fill TELEGRAM_MASTER_TOKEN first');
const base = 'https://api.telegram.org/bot' + c.masterToken;
const hook = await (await fetch(base + '/getWebhookInfo')).json() as any;
if (hook.result?.url) throw new Error('This bot already has a webhook. Use a new dedicated master bot; this command will not replace its webhook.');
const response = await (await fetch(base + '/getUpdates?timeout=0')).json() as any;
if (!response.ok) throw new Error('Telegram rejected the token');
const chats = new Map();
for (const update of response.result) {
  const m = update.message; if (m) chats.set(String(m.chat.id), { chatId: m.chat.id, type: m.chat.type, senderId: m.from?.id, topicsEnabled: m.chat.is_forum || false });
}
console.log(JSON.stringify([...chats.values()], null, 2));
if (!chats.size) console.log('Send /start privately to the new master, then /inbox inside your private forum group, and run this command again.');
