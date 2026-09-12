import { getJSON, esc, feedLink, feedTitleHTML } from '/js/common.js';

const box = document.getElementById('tags-box');
const postsBox = document.getElementById('tag-posts');
const q = new URLSearchParams(location.search);
const active = q.get('t');

try {
  const [tagsData, postsData] = await Promise.all([
    getJSON('/tags.json'),
    getJSON('/posts.json'),
  ]);

  box.innerHTML = tagsData.tags.length
    ? tagsData.tags.map(t =>
        `<a href="/tags.html?t=${encodeURIComponent(t.tag)}">#${esc(t.tag)}</a>` +
        `<span class="dim">(${t.count})</span>`
      ).join('\n')
    : 'no tags yet';

  if (active) {
    const filtered = postsData.posts.filter(p =>
      (p.tags || []).some(t => t.toLowerCase() === active.toLowerCase()));
    postsBox.innerHTML = '\n# ' + esc(active) + '\n' + (filtered.length
      ? filtered.map(p =>
          `<a href="${feedLink(p)}">${feedTitleHTML(p)}</a>` +
          `<span class="dim">${esc(p.date)}</span>`
        ).join('\n')
      : 'no posts');
  }
} catch (e) {
  box.textContent = '[Error] ' + e.message;
}