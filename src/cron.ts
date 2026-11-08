// Five-field cron matching, evaluated in an explicit IANA timezone.
function field(text: string, min: number, max: number) {
  const values = new Set<number>();
  for (const item of text.split(',')) {
    if (!/^(?:\*|\d+(?:-\d+)?)(?:\/\d+)?$/.test(item)) throw new Error('Invalid cron field');
    const pieces = item.split('/');
    if (pieces.length > 2 || (pieces[1] && !/^\d+$/.test(pieces[1]))) throw new Error('Invalid cron step');
    const step = Number(pieces[1] || 1), base = pieces[0];
    const range = base === '*' ? [min, max] : /^\d+$/.test(base) ? [Number(base), pieces[1] ? max : Number(base)] : base.split('-').map(Number);
    if (step < 1 || range.length !== 2 || range.some(n => !Number.isInteger(n) || n < min || n > max) || range[0] > range[1]) throw new Error('Invalid cron field');
    for (let n = range[0]; n <= range[1]; n += step) values.add(n);
  }
  return values;
}
export function cronSchedule(expression: string, timezone: string) {
  if (expression === 'off') return (_date: Date) => false;
  const parts = expression.trim().split(/\s+/);
  if (parts.length !== 5) throw new Error('SYNC_CRON must contain five cron fields');
  const [minute, hour, day, month, weekday] = parts.map((p, i) => field(p, [0, 0, 1, 1, 0][i], [59, 23, 31, 12, 7][i]));
  if (weekday.has(7)) weekday.add(0);
  const clock = new Intl.DateTimeFormat('en-US', { timeZone: timezone, minute: '2-digit', hour: '2-digit', day: '2-digit', month: '2-digit', weekday: 'short', hourCycle: 'h23' });
  return (date: Date) => {
    const p = Object.fromEntries(clock.formatToParts(date).map(v => [v.type, v.value]));
    const d = day.has(Number(p.day)), w = weekday.has(['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].indexOf(p.weekday));
    const calendar = parts[2] !== '*' && parts[4] !== '*' ? d || w : d && w;
    return minute.has(Number(p.minute)) && hour.has(Number(p.hour)) && month.has(Number(p.month)) && calendar;
  };
}
