import{f as u}from"./format-length-B-p6aW7q.js";import{b5 as b,bV as l,b4 as m,d as v,b6 as h,eY as y,aZ as d,b7 as C,b9 as _,b$ as $,f as a,g3 as z}from"./index-CHNBotSN.js";var x=b("icon",`
 height: 1em;
 width: 1em;
 line-height: 1em;
 text-align: center;
 display: inline-block;
 position: relative;
 fill: currentColor;
`,[l("color-transition",{transition:"color .3s var(--n-bezier)"}),l("depth",{color:"var(--n-color)"},[m("svg",{opacity:"var(--n-opacity)",transition:"opacity .3s var(--n-bezier)"})]),m("svg",{height:"1em",width:"1em"})]);const R={...h.props,depth:[String,Number],size:[Number,String],color:String,component:[Object,Function]},w=v({_n_icon__:!0,name:"Icon",inheritAttrs:!1,props:R,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:n}=_(e),t=h("Icon","-icon",x,z,e,o),i=a(()=>{const{depth:r}=e,{common:{cubicBezierEaseInOut:c},self:p}=t.value;if(r!==void 0){const{color:f,[`opacity${r}Depth`]:g}=p;return{"--n-bezier":c,"--n-color":f,"--n-opacity":g}}return{"--n-bezier":c,"--n-color":"","--n-opacity":""}}),s=n?$("icon",a(()=>`${e.depth||"d"}`),i,e):void 0;return{mergedClsPrefix:o,mergedStyle:a(()=>{const{size:r,color:c}=e;return{fontSize:u(r),color:c}}),cssVars:n?void 0:i,themeClass:s?.themeClass,onRender:s?.onRender}},render(){const{$parent:e,depth:o,mergedClsPrefix:n,component:t,onRender:i,themeClass:s}=this;return e?.$options?._n_icon__&&y("icon","don't wrap `n-icon` inside `n-icon`"),i?.(),d("i",C(this.$attrs,{role:"img",class:[`${n}-icon`,s,{[`${n}-icon--depth`]:o,[`${n}-icon--color-transition`]:o!==void 0}],style:[this.cssVars,this.mergedStyle]}),t?d(t):this.$slots.default?.())}});export{w as N};
