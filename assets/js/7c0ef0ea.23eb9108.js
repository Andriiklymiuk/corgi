"use strict";(self.webpackChunkcorgi_docs=self.webpackChunkcorgi_docs||[]).push([[4580],{9613:(e,r,n)=>{n.d(r,{Zo:()=>p,kt:()=>f});var t=n(9496);function o(e,r,n){return r in e?Object.defineProperty(e,r,{value:n,enumerable:!0,configurable:!0,writable:!0}):e[r]=n,e}function i(e,r){var n=Object.keys(e);if(Object.getOwnPropertySymbols){var t=Object.getOwnPropertySymbols(e);r&&(t=t.filter((function(r){return Object.getOwnPropertyDescriptor(e,r).enumerable}))),n.push.apply(n,t)}return n}function s(e){for(var r=1;r<arguments.length;r++){var n=null!=arguments[r]?arguments[r]:{};r%2?i(Object(n),!0).forEach((function(r){o(e,r,n[r])})):Object.getOwnPropertyDescriptors?Object.defineProperties(e,Object.getOwnPropertyDescriptors(n)):i(Object(n)).forEach((function(r){Object.defineProperty(e,r,Object.getOwnPropertyDescriptor(n,r))}))}return e}function c(e,r){if(null==e)return{};var n,t,o=function(e,r){if(null==e)return{};var n,t,o={},i=Object.keys(e);for(t=0;t<i.length;t++)n=i[t],r.indexOf(n)>=0||(o[n]=e[n]);return o}(e,r);if(Object.getOwnPropertySymbols){var i=Object.getOwnPropertySymbols(e);for(t=0;t<i.length;t++)n=i[t],r.indexOf(n)>=0||Object.prototype.propertyIsEnumerable.call(e,n)&&(o[n]=e[n])}return o}var a=t.createContext({}),l=function(e){var r=t.useContext(a),n=r;return e&&(n="function"==typeof e?e(r):s(s({},r),e)),n},p=function(e){var r=l(e.components);return t.createElement(a.Provider,{value:r},e.children)},d="mdxType",u={inlineCode:"code",wrapper:function(e){var r=e.children;return t.createElement(t.Fragment,{},r)}},m=t.forwardRef((function(e,r){var n=e.components,o=e.mdxType,i=e.originalType,a=e.parentName,p=c(e,["components","mdxType","originalType","parentName"]),d=l(n),m=o,f=d["".concat(a,".").concat(m)]||d[m]||u[m]||i;return n?t.createElement(f,s(s({ref:r},p),{},{components:n})):t.createElement(f,s({ref:r},p))}));function f(e,r){var n=arguments,o=r&&r.mdxType;if("string"==typeof e||o){var i=n.length,s=new Array(i);s[0]=m;var c={};for(var a in r)hasOwnProperty.call(r,a)&&(c[a]=r[a]);c.originalType=e,c[d]="string"==typeof e?e:o,s[1]=c;for(var l=2;l<i;l++)s[l]=n[l];return t.createElement.apply(null,s)}return t.createElement.apply(null,n)}m.displayName="MDXCreateElement"},7057:(e,r,n)=>{n.r(r),n.d(r,{assets:()=>a,contentTitle:()=>s,default:()=>u,frontMatter:()=>i,metadata:()=>c,toc:()=>l});var t=n(8957),o=(n(9496),n(9613));const i={},s="corgi run",c={unversionedId:"commands/corgi_run",id:"commands/corgi_run",title:"corgi run",description:"corgi run",source:"@site/docs/commands/corgi_run.md",sourceDirName:"commands",slug:"/commands/corgi_run",permalink:"/corgi/docs/commands/corgi_run",draft:!1,tags:[],version:"current",frontMatter:{},sidebar:"tutorialSidebar",previous:{title:"corgi pull",permalink:"/corgi/docs/commands/corgi_pull"},next:{title:"corgi test",permalink:"/corgi/docs/commands/corgi_test"}},a={},l=[{value:"corgi run",id:"corgi-run-1",level:2},{value:"Synopsis",id:"synopsis",level:3},{value:"Options",id:"options",level:3},{value:"Options inherited from parent commands",id:"options-inherited-from-parent-commands",level:3},{value:"SEE ALSO",id:"see-also",level:3},{value:"Auto generated by spf13/cobra on 13-Sep-2023",id:"auto-generated-by-spf13cobra-on-13-sep-2023",level:6}],p={toc:l},d="wrapper";function u(e){let{components:r,...n}=e;return(0,o.kt)(d,(0,t.Z)({},p,n,{components:r,mdxType:"MDXLayout"}),(0,o.kt)("h1",{id:"corgi-run"},"corgi run"),(0,o.kt)("h2",{id:"corgi-run-1"},"corgi run"),(0,o.kt)("p",null,"Run all databases and services"),(0,o.kt)("h3",{id:"synopsis"},"Synopsis"),(0,o.kt)("p",null,"This command helps to run all services and their dependent services."),(0,o.kt)("pre",null,(0,o.kt)("code",{parentName:"pre"},"corgi run [flags]\n")),(0,o.kt)("h3",{id:"options"},"Options"),(0,o.kt)("pre",null,(0,o.kt)("code",{parentName:"pre"},"      --dbServices strings   Slice of db_services to choose from.\n                             \n                             If you provide at least 1 db_service here, than corgi will choose only this db_service, while ignoring all others.\n                             none - will ignore all db_services run.\n                             (--dbServices db,db1,db2)\n                             \n                             By default all db_services are included and run.\n                                    \n  -h, --help                 help for run\n      --omit strings         Slice of parts of service to omit.\n                             \n                             beforeStart - beforeStart in services is omitted.\n                             afterStart - afterStart in services is omitted.\n                             \n                             By default nothing is omitted\n                                    \n      --pull                 Pull services repo changes\n  -s, --seed                 Seed all db_services that have seedSource or have dump.sql in their folder\n      --services strings     Slice of services to choose from.\n                             \n                             If you provide at least 1 services here, than corgi will choose only this service, while ignoring all others.\n                             none - will ignore all services run.\n                             (--services app,server)\n                             \n                             By default all services are included and run.\n                                    \n")),(0,o.kt)("h3",{id:"options-inherited-from-parent-commands"},"Options inherited from parent commands"),(0,o.kt)("pre",null,(0,o.kt)("code",{parentName:"pre"},"      --describe          Describe contents of corgi-compose file\n  -f, --filename string   Custom filepath for for corgi-compose\n      --fromScratch       Clean corgi_services folder before running\n      --silent            Hide all welcome messages\n")),(0,o.kt)("h3",{id:"see-also"},"SEE ALSO"),(0,o.kt)("ul",null,(0,o.kt)("li",{parentName:"ul"},(0,o.kt)("a",{parentName:"li",href:"corgi"},"corgi"),"\t - Corgi cli magic friend")),(0,o.kt)("h6",{id:"auto-generated-by-spf13cobra-on-13-sep-2023"},"Auto generated by spf13/cobra on 13-Sep-2023"))}u.isMDXComponent=!0}}]);