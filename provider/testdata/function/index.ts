// Returns only the variables the acceptance tests set. Neon injects branch credentials into
// process.env, and the invocation URL is public.
export default {
  fetch: (_: Request) => {
    const body = JSON.stringify({ FOO: process.env.FOO, BAR: process.env.BAR });

    return new Response(body, {
      headers: { "content-type": "application/json" },
    });
  },
};
