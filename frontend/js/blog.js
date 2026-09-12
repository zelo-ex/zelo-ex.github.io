import { getJSON, esc, feedLink, feedTitleHTML, tagsHTML } from '/js/common.js';

const list = document.getElementById('blog-list');
try {
  const data = await getJSON('/posts.json');
  list.innerHTML = data.posts.length
    ? data.posts.map(p =>
        `<a href="${feedLink(p)}">${feedTitleHTML(p)}</a>` +
        `<span class="dim">${esc(p.date)}</span>${tagsHTML(p.tags)}`
      ).join('\n')
    : 'no posts yet';
} catch (e) {
  list.textContent = '[Error] ' + e.message;
}