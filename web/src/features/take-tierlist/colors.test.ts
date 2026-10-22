import { readableTextColor } from './colors';

it('picks dark text on light and light text on dark backgrounds', () => {
  expect(readableTextColor('#ffff00')).toBe('#111111');
  expect(readableTextColor('#000080')).toBe('#ffffff');
  expect(readableTextColor('#0a0a0a')).toBe('#ffffff');
});
