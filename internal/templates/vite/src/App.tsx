export default function App() {
  return (
    <main className="min-h-screen bg-neutral-950 text-white">
      <section className="mx-auto flex min-h-screen max-w-5xl flex-col justify-center px-6 py-16">
        <p className="mb-4 text-sm font-medium uppercase tracking-wide text-cyan-300">
          Wemob
        </p>
        <h1 className="max-w-3xl text-4xl font-semibold leading-tight sm:text-6xl">
          Build the first screen here.
        </h1>
        <p className="mt-6 max-w-2xl text-base leading-7 text-neutral-300 sm:text-lg">
          This Vite frontend is static by default. Add backend routes only when
          the app needs AI, data, auth, payments, or server-side integrations.
        </p>
      </section>
    </main>
  );
}
