<script setup>
defineProps({
  name: { type: String, required: true },
  size: { type: Number, default: 18 },
})

// 24x24 outline icons, drawn with currentColor.
const paths = {
  home: ['M3 3h7v7H3z', 'M14 3h7v4h-7z', 'M14 11h7v10h-7z', 'M3 14h7v7H3z'],
  queue: ['M4 5h4v14H4z', 'M11 5h9', 'M11 9.5h9', 'M11 14.5h9', 'M11 19h9'],
  zap: ['M13 2 4 14h7l-1 8 9-12h-7l1-8z'],
  database: ['M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3z', 'M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6', 'M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3'],
  'arrow-right': ['M5 12h14', 'm13 6 6 6-6 6'],
  arrows: ['m8 3-4 4 4 4', 'M4 7h16', 'm16 13 4 4-4 4', 'M20 17H4'],
  activity: ['M22 12h-4l-3 8-6-16-3 8H2'],
  users: ['M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2', 'M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8z', 'M22 21v-2a4 4 0 0 0-3-3.9', 'M16 3.1a4 4 0 0 1 0 7.8'],
  layers: ['M12 2 2 7l10 5 10-5-10-5z', 'm2 17 10 5 10-5', 'm2 12 10 5 10-5'],
  radio: ['M12 14a2 2 0 1 0 0-4 2 2 0 0 0 0 4z', 'M16.2 7.8a6 6 0 0 1 0 8.4', 'M7.8 16.2a6 6 0 0 1 0-8.4', 'M19 5a10 10 0 0 1 0 14', 'M5 19A10 10 0 0 1 5 5'],
  alert: ['M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20z', 'M12 8v4', 'M12 16h.01'],
  disk: ['M22 12H2', 'M5.5 5h13l3.5 7v6a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-6l3.5-7z', 'M6 16h.01', 'M10 16h.01'],
  check: ['M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20z', 'm8 12 3 3 5-6'],
  cross: ['M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20z', 'm15 9-6 6', 'm9 9 6 6'],
  server: ['M3 4h18v7H3z', 'M3 13h18v7H3z', 'M7 7.5h.01', 'M7 16.5h.01'],
  clock: ['M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20z', 'M12 6v6l4 2'],
  sun: ['M12 16a4 4 0 1 0 0-8 4 4 0 0 0 0 8z', 'M12 2v2', 'M12 20v2', 'm4.9 4.9 1.4 1.4', 'm17.7 17.7 1.4 1.4', 'M2 12h2', 'M20 12h2', 'm4.9 19.1 1.4-1.4', 'm17.7 6.3 1.4-1.4'],
  moon: ['M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z'],
  menu: ['M4 6h16', 'M4 12h16', 'M4 18h16'],
  'arrow-left': ['M19 12H5', 'm11 18-6-6 6-6'],
  plus: ['M12 5v14', 'M5 12h14'],
  send: ['m22 2-7 20-4-9-9-4 20-7z', 'M22 2 11 13'],
  eraser: ['m7 21-4.3-4.3c-1-1-1-2.5 0-3.4l9.6-9.6c1-1 2.5-1 3.4 0l5.6 5.6c1 1 1 2.5 0 3.4L13 21', 'M22 21H7', 'm5 11 9 9'],
  trash: ['M3 6h18', 'M8 6V4h8v2', 'M19 6l-1 14H6L5 6', 'M10 11v6', 'M14 11v6'],
  search: ['M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16z', 'm21 21-4.3-4.3'],
  x: ['M18 6 6 18', 'm6 6 12 12'],
  lock: ['M5 11h14v10H5z', 'M8 11V7a4 4 0 0 1 8 0v4'],
  'log-out': ['M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4', 'm16 17 5-5-5-5', 'M21 12H9'],
}
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path v-for="d in paths[name] ?? []" :key="d" :d="d" />
  </svg>
</template>
