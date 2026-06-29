// Tunable parameters and static data for the atom hero animation.
// One vocabulary throughout: electron, electron line, proton, nucleus, zap, connection.

export const ATOM = {
    // canvas (SVG user units; viewBox is 0 0 480 480)
    cx: 240,
    cy: 240,
    radius: 178, // sphere radius the electron lines sit on
    electronCount: 13, // electrons, lines and protons are 1:1:1 with this
    tilt: (-20 * Math.PI) / 180, // base viewing tilt
    lineSamples: 64, // points sampled per electron line

    // global motion (ms periods)
    rotationPeriod: 96000, // one full revolution of the whole atom
    electronTravelPeriod: 44000, // electrons travelling along their lines
    tiltWobblePeriod: 52000, // gentle tilt sway
    breathMin: 0.7, // sphere radius scale at full exhale
    breathMax: 1.1, // ...at full inhale
    breathPeriod: 32000,
    protonChurnPeriod: 1360, // protons bobbing inside the nucleus
    protonChurnAmp: 1.6,

    // intro (power-on): each electron+proton pair fades in, then its line fades in
    pairStart: 300, // first pair appears
    pairStagger: 130, // gap between successive pairs
    pairFade: 350, // electron+proton fade-in
    lineFade: 300, // electron line fade-in
    lineDelayMin: 150, // beat between a pair and its line
    lineDelaySpan: 650,
    introMs: 3400, // total assemble time (zaps wait for it)

    // connecting lines (drawn when two bodies pass close on screen)
    electronConnectDist: 80, // px: electron ↔ electron
    electronConnectMax: 0.7, // peak opacity
    protonConnectDist: 95, // px: electron ↔ nearest proton
    protonConnectMax: 0.6,

    // electron-line flicker (false-contact): ~one line strobes every interval
    flickerInterval: 6000,
    flickerDur: 360, // length of one flicker burst
    flickerStrobeFreq: 0.09, // fast on/off speed during the burst

    // zaps (lightning to an electron + language label)
    zapGapMin: 4500, // gap between bursts
    zapGapSpan: 6500,
    zapCountMin: 1, // zaps per burst = min + floor(rand * span)
    zapCountSpan: 3,
    zapCascadeBase: 70, // stagger within a burst
    zapCascadeSpan: 110,

    // mouse parallax (atom leans toward the cursor)
    parallaxEase: 0.06,
    parallaxYaw: 0.5,
    parallaxTilt: 0.45,
} as const

// Languages a zap can name. SQL-family names get a bigger "wedge" beat.
export const LANGUAGES = [
    'TypeScript', 'Python', 'Go', 'T-SQL', 'Rust', 'Java', 'Ruby', 'C++',
    'dbt', 'Swift', 'Kotlin', 'Scala', 'Snowflake', 'PHP', 'Elixir', 'C#',
    'Lua', 'SQL', 'Dart', 'Erlang', 'Vue', 'Svelte', 'PL/pgSQL', 'JavaScript',
    'Objective-C', 'Pascal',
] as const

export const SQL_FAMILY = new Set<string>([
    'SQL', 'T-SQL', 'dbt', 'Snowflake', 'PL/pgSQL', 'MySQL', 'Postgres',
])

// Colours that anime.js tweens by value (CSS handles the rest via custom properties).
export const COLOR = {
    electron: '#f5c050', // electron resting fill
    electronZap: '#ff2034', // electron flash when struck
    line: '#2ea8ff', // electron line gradient
} as const
